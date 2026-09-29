package marketcalendar

import "time"

// isNationalHoliday reports whether the given date is a Japanese national
// holiday including 振替休日 and 国民の休日 (rules since 2020).
func isNationalHoliday(year int, month time.Month, day int) bool {
	if isNamedHoliday(year, month, day) {
		return true
	}
	date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	return isSubstituteHoliday(date) || isCitizensHoliday(date)
}

func isNamedHolidayOn(date time.Time) bool {
	return isNamedHoliday(date.Year(), date.Month(), date.Day())
}

// isSubstituteHoliday implements 振替休日: when a holiday falls on a
// Sunday, the next day that is not itself a holiday is a holiday. Walking
// back over the consecutive holidays before date, it is a substitute iff
// that run starts on a Sunday.
func isSubstituteHoliday(date time.Time) bool {
	for prev := date.AddDate(0, 0, -1); isNamedHolidayOn(prev); prev = prev.AddDate(0, 0, -1) {
		if prev.Weekday() == time.Sunday {
			return true
		}
	}
	return false
}

// isCitizensHoliday implements 国民の休日: a day sandwiched between two
// holidays (e.g. 2026-09-22).
func isCitizensHoliday(date time.Time) bool {
	return isNamedHolidayOn(date.AddDate(0, 0, -1)) && isNamedHolidayOn(date.AddDate(0, 0, 1))
}

// isNamedHoliday reports the statutory holidays proper, before
// substitute/citizen's holidays are derived from them. The 2020/2021
// Olympic-year relocations of 海の日/スポーツの日/山の日 are included.
func isNamedHoliday(year int, month time.Month, day int) bool {
	switch month {
	case time.January:
		return day == 1 || isNthMonday(year, month, day, 2)
	case time.February:
		return day == 11 || day == 23
	case time.March:
		return day == vernalEquinoxDay(year)
	case time.April:
		return day == 29
	case time.May:
		return day >= 3 && day <= 5
	case time.July:
		return isJulyHoliday(year, day)
	case time.August:
		return day == mountainDay(year)
	case time.September:
		return isNthMonday(year, month, day, 3) || day == autumnalEquinoxDay(year)
	case time.October:
		return year != 2020 && year != 2021 && isNthMonday(year, month, day, 2)
	case time.November:
		return day == 3 || day == 23
	}
	return false
}

// isJulyHoliday covers 海の日 (3rd Monday) plus, in the Olympic years,
// its relocated date and the relocated スポーツの日.
func isJulyHoliday(year, day int) bool {
	switch year {
	case 2020:
		return day == 23 || day == 24
	case 2021:
		return day == 22 || day == 23
	}
	return isNthMonday(year, time.July, day, 3)
}

// mountainDay is the day of August 山の日 falls on.
func mountainDay(year int) int {
	switch year {
	case 2020:
		return 10
	case 2021:
		return 8
	}
	return 11
}

// isNthMonday reports whether day of year/month is the nth Monday of it.
func isNthMonday(year int, month time.Month, day, n int) bool {
	date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	return date.Weekday() == time.Monday && (day-1)/7 == n-1
}

// vernalEquinoxDay/autumnalEquinoxDay use the standard approximation
// valid for 1980-2099.
func vernalEquinoxDay(year int) int {
	return int(20.8431 + 0.242194*float64(year-1980) - float64((year-1980)/4))
}

func autumnalEquinoxDay(year int) int {
	return int(23.2488 + 0.242194*float64(year-1980) - float64((year-1980)/4))
}
