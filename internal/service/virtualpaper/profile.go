// Package virtualpaper is the logic of the "virtual Paper" profiles (issue
// #1087): several profiles, each holding only the difference of the Policy /
// Exit settings from the real Paper (a variant.Params), are replayed over the
// same stored jev_decisions and market_snapshots, their virtual trades are
// recorded in virtual_paper_trades, and the profiles are compared over the same
// days.
//
// It is research only and record-only. The recorder reads the stored history
// through backtest.RunConfig (the replay engine, whose fill assumption is
// internal/service/fillmodel) and writes only the three virtual_paper_* tables
// through internal/repository/virtualpaper: never paper_orders, positions,
// capital, signals or a setting, and nothing here is an input of Policy, Risk,
// execution or live Jev. See docs/architecture/paper-results-mcp-virtual-paper.md.
package virtualpaper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode"
	"unicode/utf8"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/virtualpaper"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
)

// The caps of the profiles. They bound the daily replay work (one replay per
// active profile and day), so the recorder never grows with the number of
// profiles a client creates.
const (
	// MaxActiveProfiles is the number of profiles recorded at once.
	MaxActiveProfiles = 5
	// MaxProfiles is the number of profiles in total, stopped ones included.
	MaxProfiles = 20
	// MaxNameRunes is the longest profile name.
	MaxNameRunes = 40
)

// jst is the zone of the days a profile is replayed for; Japan has no DST.
var jst = time.FixedZone("JST", 9*60*60)

// InvalidError names the argument that was refused and why. Reason quotes only
// the rule, never a stored value.
type InvalidError struct {
	Field  string
	Reason string
}

func (e *InvalidError) Error() string { return fmt.Sprintf("virtualpaper: %s: %s", e.Field, e.Reason) }

// LimitError is returned when a profile cap is reached; Reason says which.
type LimitError struct{ Reason string }

func (e *LimitError) Error() string { return "virtualpaper: " + e.Reason }

// CreateArgs are the arguments of Create.
type CreateArgs struct {
	Name   string
	Params variant.Params
	// CloneFrom is the id of a profile whose params are copied first and then
	// overridden by Params; nil starts from the baseline (no override).
	CloneFrom *int64
}

// ParamsOf parses a profile's stored params_json.
func ParamsOf(p repo.Profile) (variant.Params, error) {
	var out variant.Params
	dec := json.NewDecoder(bytes.NewReader([]byte(p.ParamsJSON)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return variant.Params{}, fmt.Errorf("virtualpaper: params of profile %d: %w", p.ID, err)
	}
	return out, nil
}

// FirstDayOf is the first JST day a profile created at t is replayed for: the
// day after the creation day (the creation day itself is half over, so a profile
// only ever sees whole days).
func FirstDayOf(t time.Time) string {
	return t.In(jst).AddDate(0, 0, 1).Format(time.DateOnly)
}

// Create validates args and stores an active profile at now, in the
// transaction db is part of (the caller checks the MCP write switch in the same
// transaction). Errors: *InvalidError, *variant.InvalidError, *LimitError,
// repo.ErrNameTaken.
func Create(ctx context.Context, db repo.DB, now time.Time, client string, args CreateArgs) (repo.Profile, error) {
	if err := checkName(args.Name); err != nil {
		return repo.Profile{}, err
	}
	params := args.Params
	if args.CloneFrom != nil {
		src, err := repo.GetProfile(ctx, db, *args.CloneFrom)
		if errors.Is(err, repo.ErrNotFound) {
			return repo.Profile{}, &InvalidError{"clone_from", "その profile_id のプロファイルはありません"}
		}
		if err != nil {
			return repo.Profile{}, err
		}
		if params, err = overlay(src, args.Params); err != nil {
			return repo.Profile{}, err
		}
	}
	if err := params.Validate(); err != nil {
		return repo.Profile{}, err
	}
	total, active, err := repo.CountProfiles(ctx, db)
	if err != nil {
		return repo.Profile{}, err
	}
	if total >= MaxProfiles {
		return repo.Profile{}, &LimitError{fmt.Sprintf("プロファイルは停止したものも含めて %d 件までです", MaxProfiles)}
	}
	if active >= MaxActiveProfiles {
		return repo.Profile{}, &LimitError{fmt.Sprintf("動かせるプロファイルは同時に %d 件までです。先に virtual_paper_profile_stop で止めてください", MaxActiveProfiles)}
	}
	id, err := repo.InsertProfile(ctx, db, repo.NewProfile{
		Name: args.Name, ParamsJSON: params.Canonical(), CreatedAt: now, FirstDay: FirstDayOf(now),
		ClonedFrom: args.CloneFrom, Actor: repo.ActorMCP, Client: client,
	})
	if err != nil {
		return repo.Profile{}, err
	}
	return repo.GetProfile(ctx, db, id)
}

// Stop stops profile id at now. changed is false when it was already stopped
// (the profile is returned either way); repo.ErrNotFound for an unknown id. The
// recorded rows stay.
func Stop(ctx context.Context, db repo.DB, now time.Time, id int64) (p repo.Profile, changed bool, err error) {
	if p, err = repo.GetProfile(ctx, db, id); err != nil {
		return repo.Profile{}, false, err
	}
	if p.Status != repo.StatusActive {
		return p, false, nil
	}
	if changed, err = repo.StopProfile(ctx, db, id, now); err != nil {
		return repo.Profile{}, false, err
	}
	p, err = repo.GetProfile(ctx, db, id)
	return p, changed, err
}

// overlay is src's params with the fields set in over replacing theirs.
func overlay(src repo.Profile, over variant.Params) (variant.Params, error) {
	merged, err := ParamsOf(src)
	if err != nil {
		return variant.Params{}, err
	}
	if err := json.Unmarshal([]byte(over.Canonical()), &merged); err != nil {
		return variant.Params{}, fmt.Errorf("virtualpaper: overlay params: %w", err)
	}
	return merged, nil
}

// checkName accepts 1..MaxNameRunes characters with no control character and no
// leading or trailing space.
func checkName(name string) error {
	n := utf8.RuneCountInString(name)
	switch {
	case n < 1 || n > MaxNameRunes:
		return &InvalidError{"name", fmt.Sprintf("1 文字以上 %d 文字以下です", MaxNameRunes)}
	case !utf8.ValidString(name):
		return &InvalidError{"name", "UTF-8 の文字列です"}
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return &InvalidError{"name", "制御文字は使えません"}
		}
	}
	if first, _ := utf8.DecodeRuneInString(name); unicode.IsSpace(first) {
		return &InvalidError{"name", "先頭と末尾に空白は置けません"}
	}
	if last, _ := utf8.DecodeLastRuneInString(name); unicode.IsSpace(last) {
		return &InvalidError{"name", "先頭と末尾に空白は置けません"}
	}
	return nil
}

// ProfileView is a profile as the MCP shows it: the canonical params, no actor
// or client.
type ProfileView struct {
	ID     int64          `json:"profile_id"`
	Name   string         `json:"name"`
	Status string         `json:"status"`
	Params variant.Params `json:"params"`
	// FirstDay is the first JST day the profile is replayed for.
	FirstDay   string  `json:"first_day"`
	CreatedAt  string  `json:"created_at"`
	StoppedAt  *string `json:"stopped_at"`
	ClonedFrom *int64  `json:"cloned_from"`
}

// ViewOf renders p.
func ViewOf(p repo.Profile) (ProfileView, error) {
	params, err := ParamsOf(p)
	if err != nil {
		return ProfileView{}, err
	}
	v := ProfileView{
		ID: p.ID, Name: p.Name, Status: p.Status, Params: params, FirstDay: p.FirstDay,
		CreatedAt: p.CreatedAt.Format(time.RFC3339), ClonedFrom: p.ClonedFrom,
	}
	if p.StoppedAt != nil {
		v.StoppedAt = new(p.StoppedAt.Format(time.RFC3339))
	}
	return v, nil
}
