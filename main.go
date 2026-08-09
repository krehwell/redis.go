package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Arity struct {
	min int
	max int
}

var ARITIES = map[string]Arity{
	"PING":    {0, 1},
	"ECHO":    {1, 1},
	"COMMAND": {0, 1},
	"SET":     {2, 2},
	"GET":     {1, 1},
}

var storage = map[string]string{}

func checkArity(cmd string, args ...string) string {
	arity, ok := ARITIES[cmd]
	if !ok {
		return encodeError(fmt.Sprintf("ERR unknown command '%s'", cmd))
	}

	lo, hi := arity.min, arity.max
	if len(args) < lo || len(args) > hi {
		return encodeError(fmt.Sprintf("ERR wrong number of arguments for '%s' command", cmd))
	}

	return ""
}

func handleCommand(args []string) string {
	cmd := strings.ToUpper(args[0])

	err := checkArity(cmd, args[1:]...)
	if err != "" {
		return err
	}

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
	case "SET":
		storage[args[1]] = args[2]
		return encodeSimpleString("OK")
	case "GET":
		if v, ok := storage[args[1]]; ok {
			return encodeBulkString(v)
		}
		return encodeBulkString()
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

func parseArgs(line string) []string {
	var args []string
	var cur strings.Builder
	inQ := false
	for _, ch := range line {
		switch {
		case ch == '"' && !inQ:
			inQ = true
		case ch == '"' && inQ:
			inQ = false
		case ch == ' ' && !inQ:
			if cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(ch)
		}
	}
	if cur.Len() > 0 {
		args = append(args, cur.String())
	}
	return args
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fmt.Print(handleCommand(parseArgs(line)))
	}
}
