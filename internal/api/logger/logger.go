package logger

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type ParamPair struct {
	Key   string
	Value interface{}
}

var (
	blue   = "\x1b[34m"
	yellow = "\x1b[33m"
	red    = "\x1b[31m"
	green  = "\x1b[32m"
	purple = "\x1b[35m"
	orange = "\x1b[38;5;208m"
	reset  = "\x1b[0m"
)

var lastLogLevel string
var isFirstLog = true

const maxLogParamChars = 100

func prefix(level string) string {
	var color string
	switch strings.ToUpper(level) {
	case "DEBUG":
		color = blue
	case "INFO":
		color = green
	case "WARNING", "WARN":
		color = yellow
	case "ERROR", "ERR":
		color = red
	case "AI":
		color = purple
	case "SQL":
		color = orange
	default:
		color = reset
	}
	gray := "\x1b[90m"

	if !isFirstLog && lastLogLevel != level {
		fmt.Println()
	}
	isFirstLog = false
	lastLogLevel = level

	return fmt.Sprintf("[%s%s%s] - %s%s%s - ", color, strings.ToUpper(level), reset, gray, time.Now().Format("2006-01-02T15:04:05"), reset)
}

func formatParamsOrdered(level string, params []ParamPair) string {
	var sb strings.Builder
	var color string
	switch strings.ToUpper(level) {
	case "DEBUG":
		color = blue
	case "INFO":
		color = green
	case "WARNING", "WARN":
		color = yellow
	case "ERROR", "ERR":
		color = red
	case "AI":
		color = purple
	case "SQL":
		color = orange
	default:
		color = reset
	}

	for _, pair := range params {
		sb.WriteString(fmt.Sprintf(" > %s%s%s: %s\n", color, pair.Key, reset, truncateLogParam(pair.Value)))
	}
	return sb.String()
}

func truncateLogParam(value interface{}) string {
	raw := fmt.Sprintf("%v", value)
	runes := []rune(raw)
	if len(runes) <= maxLogParamChars {
		return raw
	}

	return string(runes[:maxLogParamChars]) + "..."
}

func SQL(format string, params []ParamPair) {
	msg := formatParamsOrdered("SQL", params)
	fmt.Printf("%s%s\n%s", prefix("SQL"), format, msg)
}

func Debugf(format string, params []ParamPair) {
	if params == nil {
		params = []ParamPair{}
	}
	msg := formatParamsOrdered("DEBUG", params)
	fmt.Printf("%s%s\n%s", prefix("DEBUG"), format, msg)
}

func Infof(format string, params []ParamPair) {
	if params == nil {
		params = []ParamPair{}
	}
	msg := formatParamsOrdered("INFO", params)
	fmt.Printf("%s%s\n%s", prefix("INFO"), format, msg)
}

func Warnf(format string, params []ParamPair) {
	if params == nil {
		params = []ParamPair{}
	}
	msg := formatParamsOrdered("WARNING", params)
	fmt.Printf("%s%s\n%s", prefix("WARNING"), format, msg)
}

func Errorf(format string, params []ParamPair) {
	if params == nil {
		params = []ParamPair{}
	}
	msg := formatParamsOrdered("ERROR", params)
	fmt.Printf("%s%s\n%s", prefix("ERROR"), format, msg)
}

func Fatalf(format string, params []ParamPair) {
	Errorf(format, params)
	os.Exit(1)
}

func AI(format string, params []ParamPair) {
	if params == nil {
		params = []ParamPair{}
	}
	msg := formatParamsOrdered("AI", params)
	fmt.Printf("%s%s\n%s", prefix("AI"), format, msg)
}
