package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Arity struct {
	min int
	max int
}

var ARITIES = map[string]Arity{
	"PING":    {0, 1},
	"ECHO":    {1, 1},
	"COMMAND": {1, 1},
	"SET":     {2, 8},
	"GET":     {1, 1},
	"DBSIZE":  {0, 0},
	"INCR":    {1, 1},
	"DECR":    {1, 1},
	"INCRBY":  {2, 2},
	"DECRBY":  {2, 2},
	"EXPIRE":  {2, 2},
	"TTL":     {1, 1},
	"PTTL":    {1, 1},
	"PERSIST": {1, 1},
	"WAIT":    {1, 1},
	"EXISTS":  {1, 1},
	"LPUSH":   {2, 128},
	"RPUSH":   {2, 128},
	"LRANGE":  {3, 3},
}

var clock int64 = 0 // simulated clock in milliseconds

var storage = map[string]string{}
var expires = map[string]time.Time{}
var lists = map[string][]string{}
var keyType = map[string]string{}

func isWrongType(key, want string) string {
	if t, ok := keyType[key]; ok && t != want {
		return encodeError("WRONGTYPE Operation against a key holding the wrong kind of value")
	}
	return ""
}

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

func handleCommand(cmd string, args []string) string {
	cmd = strings.ToUpper(cmd)

	err := checkArity(cmd, args[0:]...)
	if err != "" {
		return err
	}

	switch cmd {
	case "PING":
		if len(args) > 0 {
			return encodeBulkString(args[0])
		}
		return encodeSimpleString("PONG")
	case "ECHO":
		return encodeBulkString(args[0])
	case "COMMAND":
		if args[0] == "DOCS" {
			return encodeSimpleString("OK")
		}
	case "SET":
		return cmdSet(args[0], args[1], args[2:]...)
	case "GET":
		return cmdGet(args[0])
	case "INCR":
		return cmdAccumulate(1, args[0], "1")
	case "DECR":
		return cmdAccumulate(-1, args[0], "1")
	case "INCRBY":
		return cmdAccumulate(1, args[0], args[1])
	case "DECRBY":
		return cmdAccumulate(-1, args[0], args[1])
	case "DBSIZE":
		eagerExpirySweep()
		return encodeInteger(len(storage))
	case "EXPIRE":
		return cmdExpire(args...)
	case "TTL":
		return cmdTtl(args...)
	case "PTTL":
		return cmdPttl(args...)
	case "PERSIST":
		return cmdPersist(args...)
	case "EXISTS":
		expiryIfNeeded(args[0])
		if _, found := storage[args[0]]; found {
			return encodeInteger(1)
		}
		return encodeInteger(0)
	case "LPUSH", "RPUSH":
		return cmdPush(cmd, args[0], args[1:]...)
	case "LRANGE":
		return cmdLRange(args...)
	case "WAIT":
		ms, _ := strconv.ParseInt(args[0], 10, 64)
		clock += ms
		return encodeSimpleString("OK")
		// return encodeError("ERR time not implemented")
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
	keyType[key] = "string"

	return encodeInteger(sum)
}

func cmdPersist(args ...string) string {
	key := args[0]
	_, found := expires[key]
	if found {
		delete(expires, key)
		return encodeInteger(1)
	}
	return encodeInteger(0)
}

func cmdTtl(args ...string) string {
	key := args[0]

	if expiryIfNeeded(key) {
		return encodeInteger(-2)
	}

	if _, found := storage[key]; !found {
		return encodeInteger(-2)
	}

	exp, found := expires[key]
	if !found {
		return encodeInteger(-1)
	}

	remaining := time.Until(exp)
	return encodeInteger(int(math.Ceil(remaining.Seconds())))
}

func cmdPttl(args ...string) string {
	key := args[0]
	expiryIfNeeded(key)

	_, found := storage[key]

	if !found {
		return encodeInteger(-2)
	}

	exp, hasExp := expires[key]
	if !hasExp {
		return encodeInteger(-1)
	}

	remaining := time.Until(exp)
	return encodeInteger(int(remaining+time.Millisecond-1) / int(time.Millisecond))
}

func cmdExpire(args ...string) string {
	key := args[0]
	seconds, err := strconv.Atoi(args[1])
	if err != nil {
		return encodeError("ERR value is not an integer or out of range")
	}

	_, found := storage[key]
	if !found {
		return encodeInteger(0)
	}
	expires[key] = now().Add(time.Duration(seconds) * time.Second)
	return encodeInteger(1)
}

func eagerExpirySweep() {
	for key := range storage {
		expiryIfNeeded(key)
	}
}

func IsExpire(key string) bool {
	exp, found := expires[key]
	if !found {
		return false
	}
	return now().After(exp)
}

func expiryIfNeeded(key string) bool {
	if IsExpire(key) {
		delete(storage, key)
		delete(expires, key)
		delete(keyType, key)
		delete(lists, key)
		return true
	}
	return false
}

func cmdGet(args ...string) string {
	key := args[0]

	expiryIfNeeded(key)

	if v, found := storage[key]; found {
		return encodeBulkString(v)
	}
	return encodeNil()
}

func cmdSet(key, value string, opts ...string) string {
	var ttl time.Duration
	var hasTtl bool
	nx, xx := false, false

	for i := 0; i < len(opts); i++ {
		flag := strings.ToUpper(opts[i])
		switch flag {
		case "NX":
			nx = true
		case "XX":
			xx = true
		case "EX", "PX":
			if hasTtl || i+1 >= len(opts) {
				return encodeError("ERR syntax error")
			}
			n, err := strconv.Atoi(opts[i+1])
			if err != nil {
				return encodeError("ERR value is not an integer or out of range")
			}
			if n <= 0 {
				return encodeError("ERR invalid expire time in 'set' command")
			}
			unit := time.Second
			if flag == "PX" {
				unit = time.Millisecond
			}
			ttl = time.Duration(n) * unit
			hasTtl = true
			i++
		default:
			return encodeError("ERR syntax error")
		}
	}

	if nx && xx {
		return encodeError("ERR syntax error")
	}

	_, found := storage[key]
	if (nx && found) || (xx && !found) {
		return encodeNil()
	}

	storage[key] = value
	keyType[key] = "string"

	if hasTtl {
		expires[key] = now().Add(ttl)
	} else {
		delete(expires, key)
	}

	return encodeSimpleString("OK")
}

func encodeNil() string { return "$-1\r\n" }

func cmdLRange(args ...string) string {
	key := args[0]
	expiryIfNeeded(key)

	start, err := strconv.Atoi(args[1])
	if err != nil {
		return encodeError("ERR value is not an integer or out of range")
	}
	stop, err := strconv.Atoi(args[2])
	if err != nil {
		return encodeError("ERR value is not an integer or out of range")
	}

	list := lists[key]
	ln := len(list)
	if start < 0 {
		start = ln + start
	}
	if stop < 0 {
		stop = ln + stop
	}
	if start < 0 {
		start = 0
	}
	if stop >= ln {
		stop = ln - 1
	}
	if start > stop {
		return "*0\r\n"
	}

	sub := list[start : stop+1]
	r := fmt.Sprintf("*%d\r\n", len(sub))
	for _, v := range sub {
		r += encodeBulkString(v)
	}
	return r
}

func cmdPush(sign string, key string, args ...string) string {
	isLPush := sign == "LPUSH"
	isRPush := sign == "RPUSH"
	if !isLPush && !isRPush {
		return encodeError("ERR syntax error")
	}

	expiryIfNeeded(key)

	if err := isWrongType(key, "lists"); err != "" {
		return err
	}

	list, found := lists[key]
	if !found {
		lists[key] = []string{}
	}

	if isRPush {
		list = append(list, args...)
	} else {
		rev := slices.Clone(args)
		slices.Reverse(rev)
		list = append(rev, list...)
	}

	lists[key] = list

	return encodeInteger(len(list))
}

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

func now() time.Time {
	return time.Now().Add(time.Duration(clock) * time.Millisecond)
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
		args := parseArgs(line)
		fmt.Print(handleCommand(args[0], args[1:]))
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
