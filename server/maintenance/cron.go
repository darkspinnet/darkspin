package maintenance

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type cronField struct {
	matches    []bool
	isWildcard bool
}

type cronSchedule struct {
	fields []cronField
}

func parseCron(expression string) (cronSchedule, error) {
	parts := strings.Fields(expression)
	if len(parts) != 5 {
		return cronSchedule{}, errors.New("cron requires five numeric fields")
	}
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	fields := make([]cronField, len(parts))
	for index, part := range parts {
		field, err := parseCronField(part, limits[index][0], limits[index][1])
		if err != nil {
			return cronSchedule{}, fmt.Errorf("cronField[%d]: %w", index, err)
		}
		fields[index] = field
	}
	return cronSchedule{fields: fields}, nil
}

func parseCronField(expression string, minimum, maximum int) (cronField, error) {
	field := cronField{matches: make([]bool, maximum+1)}
	for _, term := range strings.Split(expression, ",") {
		parts := strings.Split(term, "/")
		if len(parts) > 2 {
			return cronField{}, errors.New("invalid cron step")
		}
		step := 1
		var err error
		if len(parts) == 2 {
			step, err = cronInteger(parts[1])
			if err != nil || step < 1 {
				return cronField{}, errors.New("cron step must be a positive integer")
			}
		}
		first, last := minimum, maximum
		if parts[0] == "*" {
			field.isWildcard = true
		} else {
			ranges := strings.Split(parts[0], "-")
			if len(ranges) > 2 {
				return cronField{}, errors.New("invalid cron range")
			}
			first, err = cronInteger(ranges[0])
			if err != nil {
				return cronField{}, fmt.Errorf("rangeStart: %w", err)
			}
			last = first
			if len(ranges) == 2 {
				last, err = cronInteger(ranges[1])
				if err != nil {
					return cronField{}, fmt.Errorf("rangeEnd: %w", err)
				}
			} else if len(parts) == 2 {
				last = maximum
			}
		}
		if first < minimum || last > maximum || first > last {
			return cronField{}, errors.New("cron range is outside its field limits")
		}
		for position := first; position <= last; {
			field.matches[position] = true
			if step > last-position {
				break
			}
			position += step
		}
	}
	return field, nil
}

func cronInteger(text string) (int, error) {
	if text == "" {
		return 0, errors.New("empty cron number")
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return 0, errors.New("cron fields must be numeric")
		}
	}
	number, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("numberParse: %w", err)
	}
	return number, nil
}

func (e cronSchedule) next(after time.Time) (time.Time, error) {
	next := after.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := next.AddDate(8, 0, 0)
	for next.Before(limit) {
		isMonthDay := e.fields[2].matches[next.Day()]
		weekday := int(next.Weekday())
		isWeekday := e.fields[4].matches[weekday] || weekday == 0 && e.fields[4].matches[7]
		isDay := isMonthDay && isWeekday
		if !e.fields[2].isWildcard && !e.fields[4].isWildcard {
			isDay = isMonthDay || isWeekday
		}
		if e.fields[0].matches[next.Minute()] && e.fields[1].matches[next.Hour()] &&
			e.fields[3].matches[int(next.Month())] && isDay {
			return next, nil
		}
		next = next.Add(time.Minute)
	}
	return time.Time{}, errors.New("cron has no occurrence within eight years")
}
