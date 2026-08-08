package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func handleCommand(args []string) string {
	cmd := strings.ToUpper(args[0])

	switch cmd {
	case "PING":
		if a := len(args); a > 1 {
			return encodeBulkString(args[1])
		}
		return "+PONG\r\n"
	case "ECHO":
		return encodeBulkString(args[1:]...)
	case "COMMAND":
		if args[1] == "DOCS" {
			return encodeSimpleString("OK")
		}
	}

	return fmt.Sprintf("-ERR unknown command '%s'\r\n", cmd)
}

func encodeBulkString(s ...string) string {
	if s == nil {
		return "$-1\r\n"
	}
	ss := strings.Join(s, " ")
	return fmt.Sprintf("$%d\r\n%s\r\n", len(ss), ss)
}

func encodeSimpleString(s string) string {
	return fmt.Sprintf("+%s\r\n", s)
}

func encodeError(msg string) string {
	return fmt.Sprintf("-%s\r\n", msg)
}

func encodeInteger(i int) string {
	return fmt.Sprintf(":%d\r\n", i)
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		args := parseArgs(line)
		response := handleCommand(args)
		fmt.Print(response)
	}
}

func parseArgs(line string) []string {
	var args []string
	var current strings.Builder
	inQuotes := false
	for _, ch := range line {
		switch {
		case ch == '"' && !inQuotes:
			inQuotes = true
		case ch == '"' && inQuotes:
			inQuotes = false
		case ch == ' ' && !inQuotes:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}
