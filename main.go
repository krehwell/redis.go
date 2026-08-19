package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

type Arity struct {
	min int
	max int
}

var ARITIES = map[string]Arity{
	"PING":      {0, 1},
	"ECHO":      {1, 1},
	"COMMAND":   {1, 1},
	"SET":       {2, 8},
	"GET":       {1, 1},
	"DBSIZE":    {0, 0},
	"INCR":      {1, 1},
	"DECR":      {1, 1},
	"INCRBY":    {2, 2},
	"DECRBY":    {2, 2},
	"EXPIRE":    {2, 2},
	"TTL":       {1, 1},
	"PTTL":      {1, 1},
	"PERSIST":   {1, 1},
	"WAIT":      {1, 1},
	"EXISTS":    {1, 1},
	"LPUSH":     {2, 128},
	"RPUSH":     {2, 128},
	"LPOP":      {1, 1},
	"RPOP":      {1, 1},
	"LLEN":      {1, 1},
	"LRANGE":    {3, 3},
	"TYPE":      {1, 1},
	"HSET":      {2, 128},
	"HGET":      {2, 2},
	"HGETALL":   {1, 1},
	"HEXISTS":   {2, 2},
	"HDEL":      {2, 128},
	"HLEN":      {1, 1},
	"SADD":      {2, 128},
	"SCARD":     {1, 1},
	"SISMEMBER": {2, 2},
	"SREM":      {2, 128},
}

var clock int64 = 0 // simulated clock in milliseconds

var storage = map[string]string{}
var expires = map[string]time.Time{}
var lists = map[string]*List{}
var keyType = map[string]string{}
var sets = map[string]*Set{}

// { "user:1": { "name": "alice", "email": "a@mail.com" }, "user:2", { "name": "bob", "age": "30" } }
var hashes = make(map[string]map[string]string)

type Node struct {
	val  string
	next *Node
	prev *Node
}

type List struct {
	head *Node
	tail *Node
	n    int
}

func (l *List) PushLeft(val string) int {
	node := &Node{val: val, next: l.head}
	if l.head != nil {
		l.head.prev = node
	} else {
		l.tail = node
	}
	l.head = node
	l.n++
	return l.n
}

func (l *List) PushRight(val string) int {
	node := &Node{val: val, prev: l.tail}
	if l.tail != nil {
		l.tail.next = node
	} else {
		l.head = node
	}
	l.tail = node
	l.n++
	return l.n
}

func (l *List) PopLeft() string {
	curr := l.head

	l.head = curr.next
	if l.head != nil {
		l.head.prev = nil
	} else {
		l.tail = nil
	}

	curr.prev = nil
	curr.next = nil

	l.n--
	return curr.val
}

func (l *List) PopRight() string {
	curr := l.tail

	l.tail = curr.prev
	if l.tail != nil {
		l.tail.next = nil
	} else {
		l.tail = nil
	}

	curr.prev = nil
	curr.next = nil
	l.n--
	return curr.val
}

func (l *List) Values() []string {
	out := []string{}
	for i := l.head; i != l.tail; i = i.next {
		out = append(out, i.val)
	}
	return out
}

func (l *List) Sub(start, stop int) []string {
	out := []string{}

	var p *Node = l.head
	for i := 0; i < start && p != nil; i++ {
		p = p.next
	}

	for i := start; i <= stop && p != nil; i++ {
		out = append(out, p.val)
		p = p.next
	}

	return out
}

func (l *List) Len() int { return l.n }

type Set struct {
	val map[string]bool
	n   int
}

func (s *Set) Add(v string) int {
	if s.n == 0 {
		s.val = make(map[string]bool)
	}

	_, found := s.val[v]

	if found {
		return 0
	}
	s.val[v] = true
	s.n++
	return 1
}

func (s *Set) IsMember(v string) bool {
	return s.val[v]
}

func (s *Set) Len() int { return s.n }

func (s *Set) Remove(v string) int {
	_, found := s.val[v]
	if !found {
		return 0
	}

	delete(s.val, v)
	s.n--
	return 1
}

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
	case "LPOP", "RPOP":
		return cmdPop(cmd, args[0], args[1:]...)
	case "LRANGE":
		return cmdLRange(args...)
	case "LLEN":
		return cmdLlen(args[0])
	case "TYPE":
		return cmdType(args[0])
	case "HSET":
		return cmdHSet(args[0], args[1:]...)
	case "HGET":
		return cmdHGet(args[0], args[1])
	case "HDEL":
		return cmdHDel(args[0], args[1:]...)
	case "HGETALL":
		return cmdHGetAll(args[0])
	case "HEXISTS":
		return cmdHExists(args[0], args[1])
	case "HLEN":
		return cmdHLen(args[0])
	case "SADD":
		return cmdSAdd(args[0], args[1:]...)
	case "SISMEMBER":
		return cmdSIsMember(args[0], args[1])
	case "SCARD":
		return cmdSCard(args[0])
	case "SREM":
		return cmdSRem(args[0], args[1:]...)
	case "WAIT":
		ms, _ := strconv.ParseInt(args[0], 10, 64)
		clock += ms
		return encodeSimpleString("OK")
		// return encodeError("ERR time not implemented")
	}

	return encodeError("ERR unknown command")
}

func cmdType(key string) string {
	t, found := keyType[key]
	if found {
		return encodeSimpleString(t)
	}
	return encodeSimpleString("none")
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

func cmdSRem(key string, args ...string) string {
	set, found := sets[key]
	if !found {
		return encodeInteger(0)
	}

	out := 0
	for _, v := range args {
		out += set.Remove(v)
	}

	if set.Len() == 0 {
		delete(sets, key)
		delete(keyType, key)
	}

	return encodeInteger(out)
}

func cmdSCard(key string) string {
	set, found := sets[key]
	if !found {
		return encodeInteger(0)
	}

	return encodeInteger(set.Len())
}

func cmdSIsMember(key, member string) string {
	set, found := sets[key]
	if !found {
		return encodeInteger(0)
	}

	if out := set.IsMember(member); out == true {
		return encodeInteger(1)
	} else {
		return encodeInteger(0)
	}

}

func cmdSAdd(key string, args ...string) string {
	set, found := sets[key]

	if err := isWrongType(key, "sets"); err != "" {
		return err
	}

	if !found {
		set = &Set{}
		sets[key] = set
		keyType[key] = "sets"
	}

	out := 0
	for _, v := range args {
		out += set.Add(v)
	}

	return encodeInteger(out)
}

func cmdHDel(key string, innerKeys ...string) string {
	h, found := hashes[key]
	if !found {
		return encodeInteger(0)
	}

	out := 0
	for _, innerKey := range innerKeys {
		_, found := h[innerKey]
		if !found {
			continue
		}
		delete(h, innerKey)
		out++
	}

	if len(h) == 0 {
		delete(hashes, key)
	}

	return encodeInteger(out)
}

func cmdHGetAll(key string) string {
	out := []string{}
	field := hashes[key]

	for k, v := range field {
		out = append(out, k, v)
	}

	return encodeArray(out)
}

func cmdHExists(key, innerKey string) string {
	_, found := hashes[key][innerKey]
	if found {
		return encodeInteger(1)
	}
	return encodeInteger(0)
}

func cmdHLen(key string) string {
	l := len(hashes[key])
	return encodeInteger(l)
}

func cmdHSet(key string, args ...string) string {
	if len(args)%2 != 0 {
		return encodeError("ERR params must be in key-value-pair")
	}

	h, found := hashes[key]
	if !found {
		h = make(map[string]string)
		hashes[key] = h
	}

	out := 0
	for i := 0; i < len(args); i += 2 {
		innerKey, value := args[i], args[i+1]
		if _, found := h[innerKey]; !found {
			out++
		}
		h[innerKey] = value
	}

	return encodeInteger(out)
}

func cmdHGet(key, innerKey string) string {
	if err := isWrongType(key, "hashes"); err != "" {
		return err
	}

	h, found := hashes[key]
	if !found {
		return encodeNil()
	}

	v, found := h[innerKey]
	if !found {
		return encodeNil()
	}

	return encodeBulkString(v)
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
	if list == nil {
		return encodeArray(nil)
	}
	ln := list.Len()
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
		return encodeArray(nil)
	}

	return encodeArray(list.Sub(start, stop))
}

func cmdLlen(key string) string {
	l, found := lists[key]
	if !found {
		return encodeInteger(0)
	}
	return encodeInteger(l.Len())
}

func cmdPop(sign string, key string, args ...string) string {
	isLPop := sign == "LPOP"
	isRPop := sign == "RPOP"
	if !isLPop && !isRPop {
		return encodeError("ERR syntax error")
	}

	expiryIfNeeded(key)

	if err := isWrongType(key, "lists"); err != "" {
		return err
	}

	list, found := lists[key]
	if !found || list.Len() == 0 {
		return encodeNil()
	}

	out := ""
	if isLPop {
		out = list.PopLeft()
	} else {
		out = list.PopRight()
	}

	if list.Len() == 0 {
		delete(lists, key)
		delete(keyType, key)
	}

	return encodeBulkString(out)
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
		list = &List{}
		lists[key] = list
		keyType[key] = "lists"
	}

	for _, v := range args {
		if isRPush {
			list.PushRight(v)
		} else {
			list.PushLeft(v)
		}
	}

	lists[key] = list

	return encodeInteger(list.Len())
}

func encodeArray(items []string) string {
	r := fmt.Sprintf("*%d\r\n", len(items))
	for _, v := range items {
		r += encodeBulkString(v)
	}
	return r
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
