package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Arity struct {
	min int
	max int
}

var ARITIES = map[string]Arity{
	"PING":    {0, 1},
	"ECHO":    {1, 1},
	"COMMAND": {1, 1},
	"SET":     {2, 4},
	"GET":     {1, 1},
	"DBSIZE":  {0, 0},
	"INCR":    {1, 1},
	"DECR":    {1, 1},
	"INCRBY":  {2, 2},
	"DECRBY":  {2, 2},
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
		if len(args) > 1 {
			return encodeBulkString(args[1])
		}
		return encodeSimpleString("PONG")
	case "ECHO":
		return encodeBulkString(args[1])
	case "COMMAND":
		if args[1] == "DOCS" {
			return encodeSimpleString("OK")
		}
	case "SET":
		return cmdSet(args[1], args[2], args[3:]...)
	case "GET":
		if v, found := storage[args[1]]; found {
			return encodeBulkString(v)
		}
		return NIL
	case "INCR":
		return cmdAccumulate(1, args[1], "1")
	case "DECR":
		return cmdAccumulate(-1, args[1], "1")
	case "INCRBY":
		return cmdAccumulate(1, args[1], args[2])
	case "DECRBY":
		return cmdAccumulate(-1, args[1], args[2])
	case "DBSIZE":
		return encodeInteger(len(storage))
	}

	return encodeError("ERR unknown command")
}

func cmdAccumulate(sign int, key, amount string) string {
	v, found := storage[key]
	if !found {
		v = "0"
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		return encodeError("ERR value is not an integer or out of range")
	}

	add, err := strconv.Atoi(amount)
	if err != nil {
		return encodeError("ERR value is not an integer or out of range")
	}

	sum := n + sign*add
	storage[key] = strconv.Itoa(sum)

	return encodeInteger(sum)
}

func cmdSet(key, value string, opts ...string) string {
	if len(opts) > 1 {
		return encodeError("ERR syntax error")
	}

	if len(opts) == 1 {
		_, found := storage[key]
		switch strings.ToUpper(opts[0]) {
		case "NX":
			if found {
				return NIL
			}
		case "XX":
			if !found {
				return NIL
			}
		default:
			return encodeError("ERR syntax error")
		}
	}

	storage[key] = value
	return encodeSimpleString("OK")
}

const NIL = "$-1\r\n"

func encodeBulkString(s string) string {
	return fmt.Sprintf("$%d\r\n%s\r\n", len(s), s)
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

// func main() {
// 	r := bufio.NewReader(os.Stdin)
// 	w := bufio.NewWriter(os.Stdout)
// 	defer w.Flush()
// 	for {
// 		args, err := parseRequest(r)
// 		if err != nil {
// 			return
// 		}
// 		w.WriteString(handleCommand(args))
// 		w.Flush()
// 	}
// }
//
// func readCount(r *bufio.Reader, prefix byte) (int, error) {
// 	line, err := r.ReadString('\n')
// 	if err != nil {
// 		return 0, err
// 	}
// 	if len(line) == 0 || line[0] != prefix {
// 		return 0, fmt.Errorf("expected %q, got %q", prefix, line)
// 	}
// 	return strconv.Atoi(strings.TrimRight(line[1:], "\r\n"))
// }
//
// func parseRequest(r *bufio.Reader) ([]string, error) {
// 	n, err := readCount(r, '*')
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	args := make([]string, n)
// 	for i := range args {
// 		length, err := readCount(r, '$')
// 		if err != nil {
// 			return nil, err
// 		}
// 		buf := make([]byte, length+2)
// 		if _, err := io.ReadFull(r, buf); err != nil {
// 			return nil, err
// 		}
// 		args[i] = string(buf[:length])
// 	}
// 	return args, nil
// }
