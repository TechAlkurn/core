package lib

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func GetFirstAndLastDay(month, year, format string) (int64, int64) {
	months := map[string]time.Month{
		"Jan": time.January, "January": time.January, "1": time.January,
		"Feb": time.February, "February": time.February, "2": time.February,
		"Mar": time.March, "March": time.March, "3": time.March,
		"Apr": time.April, "April": time.April, "4": time.April,
		"May": time.May, "5": time.May,
		"Jun": time.June, "June": time.June, "6": time.June,
		"Jul": time.July, "July": time.July, "7": time.July,
		"Aug": time.August, "August": time.August, "8": time.August,
		"Sep": time.September, "September": time.September, "9": time.September,
		"Oct": time.October, "October": time.October, "10": time.October,
		"Nov": time.November, "November": time.November, "11": time.November,
		"Dec": time.December, "December": time.December, "12": time.December,
	}
	firstDay := time.Date(ToInt(year), months[month], 1, 0, 0, 0, 0, time.UTC)
	lastDay := firstDay.AddDate(0, 1, 0).Add(-time.Second)
	return firstDay.Unix(), lastDay.Unix()
}

func ParseTimeString(str string) (int64, error) {
	// Split the string on colon (":") to get hour and minute
	parts := strings.Split(str, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid time format: %s", str)
	}

	// Convert hour and minute strings to integers
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}

	// Get current time with location set to UTC (adjust if needed)
	now := time.Now().In(time.UTC)

	// Extract year, month, day from current time
	year, month, day := now.Date()
	// Create a time object for today's desired time
	targetTime := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)

	// Convert target time to Unix timestamp (seconds since Unix epoch)
	return targetTime.Unix(), nil
}

func Strtotime(str string) (int64, error) {
	// layout := "2006-01-02 15:04:05"
	t, err := time.Parse(time.DateTime, str)
	if err != nil {
		return 0, err
	}
	return t.Unix(), nil
}

func DateFormat(layout, str string) string {
	parsedTime, _ := time.Parse(time.RFC3339, str)
	return parsedTime.Format(layout)
}

func ParseTime(str string) string {
	parsedTime, _ := time.Parse(time.RFC3339, str)
	return parsedTime.Format(time.DateTime)
}

func Microtime() int64 {
	return time.Now().UnixNano() / 1e6
}

func Time() int64 {
	return time.Now().Unix()
}

func DateTime() string {
	return time.Now().Format(time.DateTime)
}

// time id millseconds
func TimeToDateFormat(sec, nsec int64) string {
	return time.Unix(sec, nsec).Format(time.DateTime)
}
