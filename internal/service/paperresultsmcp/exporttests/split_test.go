package exporttests

import (
	"path/filepath"
	"slices"
	"testing"
)

// A split sends the days before split_at to train and the days from split_at + embargo_days to eval,
// counts the embargo gap, drops the train rows whose label window reaches the eval start, and the
// four counts always add up to the decisions of the period.
func TestSplitSidesNeverOverlapAndEveryDecisionIsAccountedFor(t *testing.T) {
	decs := fixture()
	total := len(tradersInPeriod(decs))
	cases := []struct {
		name                string
		embargo             int
		train, eval         []int // expected ids
		embargoRows, purged int
		trainDays, evalDays [2]string
	}{
		// id 7 (23:50 JST on day 7, 30-minute label) is still labelling at the eval start (day 8, 00:00): purged.
		{name: "no embargo", embargo: 0, train: []int{1, 2, 3, 4, 5, 6}, eval: []int{10, 9, 11, 12, 13, 14, 17}, purged: 1,
			trainDays: [2]string{"2026-10-05", "2026-10-07"}, evalDays: [2]string{"2026-10-08", "2026-10-11"}},
		// A day of embargo: the eval starts on day 9, id 7's label ends at 00:20 on day 8: kept; day 8 is the gap.
		{name: "one day embargo", embargo: 1, train: []int{1, 2, 3, 4, 5, 6, 7}, eval: []int{11, 12, 13, 14, 17}, embargoRows: 2,
			trainDays: [2]string{"2026-10-05", "2026-10-07"}, evalDays: [2]string{"2026-10-09", "2026-10-11"}},
		{name: "three days embargo", embargo: 3, train: []int{1, 2, 3, 4, 5, 6, 7}, eval: []int{14, 17}, embargoRows: 5,
			trainDays: [2]string{"2026-10-05", "2026-10-07"}, evalDays: [2]string{"2026-10-11", "2026-10-11"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, w := newServer(t, "")
			seed(t, w, decs)
			enableExport(t, w)
			args := map[string]any{"split_at": "2026-10-08"}
			if c.embargo > 0 {
				args["embargo_days"] = c.embargo
			}
			resp := srv.call(t, args).export(t)
			m := resp.Manifest
			if !resp.Written || m == nil {
				t.Fatalf("response %+v", resp)
			}
			if m.RowsTrain != len(c.train) || m.RowsEval != len(c.eval) || m.RowsInEmbargo != c.embargoRows || m.RowsPurged != c.purged {
				t.Errorf("counts train %d eval %d embargo %d purged %d, want %d %d %d %d", m.RowsTrain, m.RowsEval, m.RowsInEmbargo, m.RowsPurged, len(c.train), len(c.eval), c.embargoRows, c.purged)
			}
			if m.RowsTrain+m.RowsEval+m.RowsInEmbargo+m.RowsPurged != total || m.Decisions != total || m.Rows != m.RowsTrain+m.RowsEval {
				t.Errorf("the counts do not add up to the %d decisions: %+v", total, m)
			}
			if m.SplitAt == nil || *m.SplitAt != "2026-10-08" || m.EmbargoDays != c.embargo {
				t.Errorf("split %v embargo %d", m.SplitAt, m.EmbargoDays)
			}
			dir := singleDataset(t, srv, m.DatasetID)
			train := parseCSV(t, readFile(t, filepath.Join(dir, "dataset_train.csv")))
			eval := parseCSV(t, readFile(t, filepath.Join(dir, "dataset_eval.csv")))
			if got := train.ids(t); !slices.Equal(got, c.train) {
				t.Errorf("train ids %v, want %v", got, c.train)
			}
			if got := eval.ids(t); !slices.Equal(got, c.eval) {
				t.Errorf("eval ids %v, want %v", got, c.eval)
			}
			if m.TrainDays.First != c.trainDays[0] || m.TrainDays.Last != c.trainDays[1] || m.EvalDays.First != c.evalDays[0] || m.EvalDays.Last != c.evalDays[1] {
				t.Errorf("days train %+v eval %+v", m.TrainDays, m.EvalDays)
			}
			// The sides never overlap in time: the last train day is before split_at, the first eval day is
			// at or after split_at + embargo_days, and a day is on one side only.
			lastTrain, firstEval := "", "9999"
			days := map[string]string{}
			for side, tb := range map[string]table{"train": train, "eval": eval} {
				for i := range tb.rows {
					day := tb.cell(t, i, "decision_day")
					if tb.cell(t, i, "split") != side {
						t.Errorf("a %s file row says split=%q", side, tb.cell(t, i, "split"))
					}
					if prev, ok := days[day]; ok && prev != side {
						t.Errorf("day %s is in both train and eval", day)
					}
					days[day] = side
					if side == "train" && day > lastTrain {
						lastTrain = day
					}
					if side == "eval" && day < firstEval {
						firstEval = day
					}
				}
			}
			if lastTrain >= "2026-10-08" || firstEval < "2026-10-08" || (c.embargo > 0 && firstEval < c.evalDays[0]) || lastTrain >= firstEval {
				t.Errorf("train ends %s, eval starts %s", lastTrain, firstEval)
			}
			// The two files together hold exactly the rows they should: eval keeps its own rows, hashes verify.
			if len(m.Files) != 4 || m.Files[0].Name != "dataset_train.csv" || m.Files[1].Name != "dataset_train.parquet" ||
				m.Files[2].Name != "dataset_eval.csv" || m.Files[3].Name != "dataset_eval.parquet" {
				t.Fatalf("files %+v", m.Files)
			}
			for _, f := range m.Files {
				if b := readFile(t, filepath.Join(dir, f.Name)); sha(b) != f.SHA256 || len(b) != f.Bytes {
					t.Errorf("%s does not hash to the manifest", f.Name)
				}
			}
			var lines string
			for _, f := range m.Files {
				lines += f.Name + " " + f.SHA256 + "\n"
			}
			if overall := sha([]byte(specText("2", periodFrom, periodTo, "2026-10-08", c.embargo) + lines)); m.DatasetSHA256 != overall {
				t.Errorf("dataset hash %s, want %s", m.DatasetSHA256, overall)
			}
		})
	}
}

// The boundary row: a train-side decision whose label window runs past the eval start is the only one purged,
// and it is the label's horizon that decides, not the day: the same decision with no label is kept.
func TestPurgeFollowsTheLabelWindow(t *testing.T) {
	srv, w := newServer(t, "")
	decs := fixture()
	for i := range decs {
		if decs[i].id == 7 {
			decs[i].calib = nil // no label: its window is only the decision instant
		}
	}
	seed(t, w, decs)
	enableExport(t, w)
	m := srv.call(t, map[string]any{"split_at": "2026-10-08"}).export(t).Manifest
	if m.RowsPurged != 0 || m.RowsTrain != 7 {
		t.Errorf("a decision without a label was purged: %+v", m)
	}
}
