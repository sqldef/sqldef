package schema

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// canonicalizeMoneyText normalizes a money literal to a plain fixed-point decimal so
// `money '1.00'`, `'1.00'::money` and PG's read-back `'$1.00'::money` converge.
// Assumes a two-decimal currency (lc_monetary like en_US); other precisions keep diffing.
func canonicalizeMoneyText(s string) (string, bool) {
	s = strings.TrimSpace(s)
	neg := false
	// Accounting notation: ($1.00) means negative.
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg = true
		s = s[1 : len(s)-1]
	}
	var digits strings.Builder
	sawDigit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
			sawDigit = true
		case r == '.':
			digits.WriteRune(r)
		case r == '-':
			neg = !neg
		default:
			// Skip currency symbol, thousands separator, spaces, etc.
		}
	}
	if !sawDigit {
		return "", false
	}
	intPart, fracPart, _ := strings.Cut(digits.String(), ".")
	if intPart == "" {
		intPart = "0"
	}
	if len(fracPart) < 2 {
		fracPart += strings.Repeat("0", 2-len(fracPart))
	} else {
		fracPart = fracPart[:2]
	}
	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	out := intPart + "." + fracPart
	if neg && out != "0.00" {
		out = "-" + out
	}
	return out, true
}

// intervalParts is PostgreSQL's interval model: months/days/micros are kept separate
// because their length is context dependent and not collapsed into seconds.
type intervalParts struct {
	months int64
	days   int64
	micros int64
}

const microsPerSecond = 1000000

var intervalUnitToMonths = map[string]int64{
	"century": 1200, "centurie": 1200,
	"decade": 120,
	"year":   12, "yr": 12,
	"month": 1, "mon": 1,
}
var intervalUnitToDays = map[string]int64{
	"week": 7, "wk": 7,
	"day": 1,
}
var intervalUnitToMicros = map[string]int64{
	"hour": 3600 * microsPerSecond, "hr": 3600 * microsPerSecond,
	"minute": 60 * microsPerSecond, "min": 60 * microsPerSecond,
	"second": microsPerSecond, "sec": microsPerSecond,
	"millisecond": 1000, "msec": 1000,
	"microsecond": 1, "usec": 1,
}

// parseIntervalText parses both PG's emitted forms ("1 year", "01:30:00", "1-2") and
// the "<number> <unit>" spellings of typed literals. ok=false leaves the expression
// untouched (it keeps diffing, no worse than before).
func parseIntervalText(s string) (intervalParts, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return intervalParts{}, false
	}
	var p intervalParts
	fields := strings.Fields(s)
	i := 0
	for i < len(fields) {
		f := fields[i]
		if strings.Contains(f, ":") {
			micros, ok := parseClockTime(f)
			if !ok {
				return intervalParts{}, false
			}
			p.micros += micros
			i++
			continue
		}
		// Year-month component "Y-M" (e.g. "1-2").
		if dash := strings.IndexByte(f, '-'); dash > 0 {
			y, errY := strconv.ParseInt(f[:dash], 10, 64)
			m, errM := strconv.ParseInt(f[dash+1:], 10, 64)
			if errY == nil && errM == nil {
				p.months += y*12 + m
				i++
				continue
			}
			return intervalParts{}, false
		}
		// A bare number with no following unit means seconds (PostgreSQL: '5'::interval).
		num := f
		unit := "second"
		consumed := 1
		if i+1 < len(fields) && !isNumericField(fields[i+1]) && !strings.Contains(fields[i+1], ":") {
			unit = fields[i+1]
			consumed = 2
		}
		if !addIntervalUnit(&p, num, unit) {
			return intervalParts{}, false
		}
		i += consumed
	}
	return p, true
}

func isNumericField(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func parseClockTime(f string) (int64, bool) {
	neg := false
	if strings.HasPrefix(f, "-") {
		neg = true
		f = f[1:]
	}
	parts := strings.Split(f, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	h, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, false
	}
	m, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, false
	}
	micros := (h*3600 + m*60) * microsPerSecond
	if len(parts) == 3 {
		sec, ok := parseFractionalSeconds(parts[2])
		if !ok {
			return 0, false
		}
		micros += sec
	}
	if neg {
		micros = -micros
	}
	return micros, true
}

// addIntervalUnit applies "<num> <unit>" to p. Like PostgreSQL a fractional value
// cascades into smaller fields (1.5 days -> 1 day + 12:00:00, 30-day months).
func addIntervalUnit(p *intervalParts, num, unit string) bool {
	unit = depluralizeUnit(unit)
	val, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return false
	}
	if mult, ok := intervalUnitToMonths[unit]; ok {
		total := val * float64(mult)
		whole := math.Trunc(total)
		p.months += int64(whole)
		fracDays := (total - whole) * 30
		wholeDays := math.Trunc(fracDays)
		p.days += int64(wholeDays)
		p.micros += int64(math.Round((fracDays - wholeDays) * 24 * 3600 * microsPerSecond))
		return true
	}
	if mult, ok := intervalUnitToDays[unit]; ok {
		total := val * float64(mult)
		whole := math.Trunc(total)
		p.days += int64(whole)
		p.micros += int64(math.Round((total - whole) * 24 * 3600 * microsPerSecond))
		return true
	}
	if mult, ok := intervalUnitToMicros[unit]; ok {
		p.micros += int64(math.Round(val * float64(mult)))
		return true
	}
	return false
}

func depluralizeUnit(u string) string {
	if len(u) > 1 && strings.HasSuffix(u, "s") {
		return u[:len(u)-1]
	}
	return u
}

func parseFractionalSeconds(s string) (int64, bool) {
	intStr, fracStr, hasFrac := strings.Cut(s, ".")
	sec, err := strconv.ParseInt(intStr, 10, 64)
	if err != nil {
		return 0, false
	}
	micros := sec * microsPerSecond
	if hasFrac {
		if len(fracStr) > 6 {
			fracStr = fracStr[:6]
		}
		fracStr += strings.Repeat("0", 6-len(fracStr))
		f, err := strconv.ParseInt(fracStr, 10, 64)
		if err != nil {
			return 0, false
		}
		micros += f
	}
	return micros, true
}

// formatIntervalPG renders PostgreSQL's default-IntervalStyle interval_out: year/month,
// day and HH:MM:SS[.ffffff] parts, each only when non-zero, singular only when value == 1.
func formatIntervalPG(p intervalParts) string {
	var segs []string
	years := p.months / 12
	mons := p.months % 12
	if years != 0 {
		segs = append(segs, fmt.Sprintf("%d %s", years, plural(years, "year")))
	}
	if mons != 0 {
		segs = append(segs, fmt.Sprintf("%d %s", mons, plural(mons, "mon")))
	}
	if p.days != 0 {
		segs = append(segs, fmt.Sprintf("%d %s", p.days, plural(p.days, "day")))
	}
	if p.micros != 0 || len(segs) == 0 {
		segs = append(segs, formatIntervalTime(p.micros))
	}
	return strings.Join(segs, " ")
}

func formatIntervalTime(micros int64) string {
	neg := ""
	if micros < 0 {
		neg = "-"
		micros = -micros
	}
	hours := micros / (3600 * microsPerSecond)
	micros -= hours * 3600 * microsPerSecond
	mins := micros / (60 * microsPerSecond)
	micros -= mins * 60 * microsPerSecond
	secs := micros / microsPerSecond
	frac := micros % microsPerSecond
	out := fmt.Sprintf("%s%02d:%02d:%02d", neg, hours, mins, secs)
	if frac != 0 {
		fs := strings.TrimRight(fmt.Sprintf("%06d", frac), "0")
		out += "." + fs
	}
	return out
}

func plural(n int64, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func canonicalizeIntervalText(s string) (string, bool) {
	p, ok := parseIntervalText(s)
	if !ok {
		return "", false
	}
	return formatIntervalPG(p), true
}
