package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Arity struct {
	min int
	max int
}

var ARITIES = map[string]Arity{
	"PING":            {0, 1},
	"ECHO":            {1, 1},
	"COMMAND":         {1, 1},
	"SET":             {2, 8},
	"GET":             {1, 1},
	"DBSIZE":          {0, 0},
	"INCR":            {1, 1},
	"DECR":            {1, 1},
	"INCRBY":          {2, 2},
	"DECRBY":          {2, 2},
	"EXPIRE":          {2, 2},
	"TTL":             {1, 1},
	"PTTL":            {1, 1},
	"PERSIST":         {1, 1},
	"WAIT":            {1, 1},
	"EXISTS":          {1, 128},
	"DEL":             {1, 128},
	"KEYS":            {1, 1},
	"TYPE":            {1, 1},
	"RENAME":          {2, 2},
	"LPUSH":           {2, 128},
	"RPUSH":           {2, 128},
	"LPOP":            {1, 1},
	"RPOP":            {1, 1},
	"LLEN":            {1, 1},
	"LRANGE":          {3, 3},
	"HSET":            {2, 128},
	"HGET":            {2, 2},
	"HGETALL":         {1, 1},
	"HEXISTS":         {2, 2},
	"HDEL":            {2, 128},
	"HLEN":            {1, 1},
	"SADD":            {2, 128},
	"SCARD":           {1, 1},
	"SISMEMBER":       {2, 2},
	"SREM":            {2, 128},
	"ZADD":            {3, 128},
	"ZRANGE":          {3, 4},
	"ZSCORE":          {2, 2},
	"ZCARD":           {1, 1},
	"ZRANK":           {2, 2},
	"MULTI":           {0, 0},
	"EXEC":            {0, 0},
	"DISCARD":         {0, 0},
	"SUBSCRIBE":       {1, 128},
	"PUBLISH":         {2, 2},
	"UNSUBSCRIBE":     {0, 128},
	"SAVE":            {0, 0},
	"RESTORE":         {1, 1},
	"AOF":             {1, 1},
	"MAXKEYS":         {1, 1},
	"INFO":            {1, 1},
	"WATCH":           {1, 128},
	"UNWATCH":         {0, 0},
	"EVAL":            {2, 128},
	"REPLICAOF":       {2, 2},
	"ROLE":            {0, 0},
	"REPLICATION_LOG": {0, 0},
	"PSYNC":           {2, 2},
	"XADD":            {3, 128},
	"XLEN":            {1, 1},
	"XRANGE":          {3, 3},
}

var clock int64 = 0 // simulated clock in milliseconds

var storage = map[string]string{}
var expires = map[string]time.Time{}
var keyTypes = map[string]string{}
var lists = map[string]*List{}
var sets = map[string]*Set{}
var zsets = map[string]*ZSet{}
var hashes = make(
	map[string]map[string]string,
) // { "user1": { "name": "alice", "email": "a@mail.com" }, "user2", { "name": "bob", "age": "30" } }
var channelSubscriber = map[string][]string{}
var mySubscriptions = Set{}

var WRITE_COMMANDS = []string{
	"SET", "DEL", "LPUSH", "RPUSH", "LPOP", "RPOP",
	"HSET", "HDEL", "SADD", "SREM", "ZADD",
	"EXPIRE", "RENAME", "RESTORE",
}

var GET_COMMANDS = []string{
	"GET", "EXISTS", "TTL", "PTTL", "HGET", "HGETALL",
	"HEXISTS", "LRANGE", "LLEN", "SISMEMBER", "SCARD",
	"ZRANGE", "ZSCORE", "ZCARD", "KEYS",
}

var accessTimes = make(map[string]time.Time)

const REPLID = "abc0000000000000000000000000000000000000"

var backlog = []string{}

type QueuedCmd struct {
	cmd  string
	args []string
}

type ClientState struct {
	isInMulti bool
	queued    []QueuedCmd
	aofOn     bool
	aofLog    []string
	maxKeys   int
	version   map[string]int
	watched   map[string]int
	role      string
}

func contains(slice []string, target string) bool {
	for _, item := range slice {
		if item == target {
			return true
		}
	}
	return false
}

func (c *ClientState) Dispatch(cmd string, args ...string) string {
	if c.isInMulti && !contains([]string{"EXEC", "DISCARD", "MULTI"}, cmd) {
		c.queued = append(c.queued, QueuedCmd{cmd, args})
		return encodeSimpleString("QUEUED")
	}

	cmd = strings.ToUpper(cmd)

	err := checkArity(cmd, args[0:]...)
	if err != "" {
		return err
	}

	if contains(WRITE_COMMANDS, cmd) {
		if len(args) > 0 {
			c.Bump(args[0])
		}

		if c.aofOn {
			c.aofLog = append(c.aofLog, cmd+" "+strings.Join(args, " "))
		}
	}

	if len(args) > 0 {
		c.TryTouch(cmd, args[0])
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
		return cmdExists(args...)
	case "DEL":
		return cmdDel(args...)
	case "KEYS":
		return cmdKeys(args...)
	case "RENAME":
		return cmdRename(args[0], args[1])
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
	case "ZADD":
		return cmdZAdd(args[0], args[1:]...)
	case "ZRANGE":
		return cmdZRange(args[0], args[1], args[2])
	case "ZSCORE":
		return cmdZScore(args[0], args[1])
	case "ZCARD":
		return cmdZCard(args[0])
	case "ZRANK":
		return cmdZRank(args[0], args[1])
	case "WAIT":
		ms, _ := strconv.ParseInt(args[0], 10, 64)
		clock += ms
		return encodeSimpleString("OK")
	case "MULTI":
		return cmdMulti(c)
	case "DISCARD":
		return cmdDiscard(c)
	case "EXEC":
		return cmdExec(c)
	case "WATCH":
		return cmdWatch(c, args...)
	case "UNWATCH":
		return cmdUnWatch(c)
	case "SUBSCRIBE":
		return cmdSubscribe(args...)
	case "PUBLISH":
		return cmdPublish(args[0], args[1])
	case "UNSUBSCRIBE":
		return cmdUnsubscribe(args...)
	case "SAVE":
		return cmdSave()
	case "RESTORE":
		return cmdRestore(args[0])
	case "AOF":
		return cmdAof(c, args[0])
	case "MAXKEYS":
		return cmdMaxKeys(c, args[0])
	case "INFO":
		return cmdInfo(c, args...)
	case "EVAL":
		return cmdEval(c, args[0], args[1], args[2:]...)
	case "REPLICAOF":
		return cmdReplicaOf(c, args...)
	case "ROLE":
		return cmdRole(c)
	case "REPLICATION_LOG":
		return cmdReplicationLog()
	case "PSYNC":
		return cmdPSync(args...)
	case "XADD":
		return cmdXAdd(args[0], args[1], args[2:]...)
	case "XLEN":
		return cmdXLen(args[0])
	case "XRANGE":
		return cmdXRange(args[0], args[1], args[2])
	}

	return encodeError(fmt.Sprintf("ERR unknown command: %s", cmd))
}

func (c *ClientState) Bump(key string) {
	c.version[key] += 1
}

func cmdWatch(client *ClientState, keys ...string) string {
	if client.isInMulti {
		return encodeNil()
	}

	for _, k := range keys {
		client.watched[k] = client.version[k]
	}

	return encodeSimpleString("OK")
}

func cmdUnWatch(client *ClientState) string {
	for k := range client.version {
		delete(client.version, k)
		delete(client.watched, k)
	}
	return encodeSimpleString("OK")
}

func cmdMulti(client *ClientState) string {
	if client.isInMulti {
		return encodeError("ERR MULTI calls can not be nested")
	}
	client.isInMulti = true
	client.queued = []QueuedCmd{}
	return encodeSimpleString("OK")
}

func cmdExec(client *ClientState) string {
	if !client.isInMulti {
		return encodeError("ERR EXEC without MULTI")
	}

	for k := range client.watched {
		if client.watched[k] != client.version[k] {
			return encodeNil()
		}
	}

	client.isInMulti = false
	queue := client.queued
	client.queued = nil
	out := fmt.Sprintf("*%d\r\n", len(queue))
	for _, q := range queue {
		cmd, args := q.cmd, q.args
		out += client.Dispatch(cmd, args...)
	}
	cmdUnWatch(client)
	return out
}

func cmdDiscard(client *ClientState) string {
	client.isInMulti = false
	client.queued = []QueuedCmd{}
	return encodeSimpleString("OK")
}

func (c *ClientState) TryTouch(cmd, key string) {
	runCleaner := false
	if contains(WRITE_COMMANDS, cmd) {
		runCleaner = true
		accessTimes[key] = now()
	}

	if contains(GET_COMMANDS, cmd) {
		if _, found := keyTypes[key]; found {
			runCleaner = true
			accessTimes[key] = now()
		}
	}

	for runCleaner && c.maxKeys > 0 && len(accessTimes) > c.maxKeys {
		victims := make([]string, 0, len(accessTimes))
		for k := range accessTimes {
			victims = append(victims, k)
		}
		sort.Slice(victims, func(i, j int) bool {
			return accessTimes[victims[i]].Before(accessTimes[victims[j]])
		})

		keyToRemove := victims[0]
		delete(storage, keyToRemove)
		delete(lists, keyToRemove)
		delete(hashes, keyToRemove)
		delete(sets, keyToRemove)
		delete(zsets, keyToRemove)
		delete(keyTypes, keyToRemove)
		delete(expires, keyToRemove)
		delete(accessTimes, keyToRemove)
	}
}

func NewClientState() *ClientState {
	return &ClientState{
		isInMulti: false,
		queued:    []QueuedCmd{},
		aofOn:     false,
		aofLog:    []string{},
		maxKeys:   0,
		version:   make(map[string]int),
		watched:   make(map[string]int),
		role:      "master",
	}
}

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
	for i := l.head; i != nil; i = i.next {
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

type ZSet struct {
	scores map[string]float64
	n      int
}

func (z *ZSet) Add(s float64, v string) int {
	_, found := z.scores[v]

	if z.n == 0 {
		z.scores = make(map[string]float64)
	}

	if found {
		return 0
	}
	z.scores[v] = s
	z.n++
	return 1
}

func (z *ZSet) GetSorted() []string {
	members := make([]string, 0, len(z.scores))

	for m := range z.scores {
		members = append(members, m)
	}

	sort.Slice(members, func(i, j int) bool {
		si, sj := z.scores[members[i]], z.scores[members[j]]
		if si != sj {
			return si < sj
		}

		return members[i] < members[j]
	})
	return members
}

func (z *ZSet) Len() int {
	return z.n
}

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

func (s *Set) Values() []string {
	out := make([]string, 0, s.Len())
	for k := range s.val {
		out = append(out, k)
	}
	return out
}

func cmdPSync(args ...string) string {
	out := ""
	if args[0] == "?" && args[1] == "1" {
		// full resync
		out += fmt.Sprintf("+FULLRESYNC %s 0\r\n", REPLID)
		out += fmt.Sprintf("$%d\r\n", len(backlog))
		out += encodeBulkList(backlog)
		return out
	}

	return "+CONTINUE\r\n"
}

func cmdReplicationLog() string {
	return encodeBulkList(backlog) + encodeSimpleString("OK")
}

func cmdRole(client *ClientState) string {
	return encodeBulkString(client.role)
}

func cmdReplicaOf(client *ClientState, args ...string) string {
	if strings.ToUpper(args[0]) == "NO" && strings.ToUpper(args[1]) == "ONE" {
		client.role = "master"
	} else {
		client.role = "replica"
	}
	return encodeSimpleString("OK")
}

func cmdEval(client *ClientState, script string, nKey string, args ...string) string {
	if strings.HasPrefix(script, "return '") && strings.HasSuffix(script, "'") {
		out := script[len("return '") : len(script)-1]
		return encodeSimpleString(out)
	}

	numOfKeys, err := strconv.Atoi(nKey)
	if err != nil {
		return encodeError("ERR value is not an integer or out of range")
	}

	keys := args[0:numOfKeys]
	argv := args[numOfKeys:]

	switch script {
	case "return redis.call('GET', KEYS[1])":
		return client.Dispatch("GET", keys[0])
	case "return redis.call('SET', KEYS[1], ARGV[1])":
		return client.Dispatch("SET", keys[0], argv[0])
	case "return redis.call('INCR', KEYS[1])":
		return client.Dispatch("INCR", keys[0])
	case "return tonumber(redis.call('GET', KEYS[1])) or 0":
		return client.Dispatch("GET", keys[0])
	case "return #KEYS":
		return encodeInteger(len(keys))
	case "return ARGV[1]":
		return encodeBulkString(argv[0])
	}
	return ""
}

func cmdMaxKeys(client *ClientState, max string) string {
	m, err := strconv.Atoi(max)
	if err != nil {
		return encodeError("ERR value is not an integer or out of range")
	}
	client.maxKeys = m
	return encodeSimpleString("OK")
}

func cmdInfo(client *ClientState, args ...string) string {
	want := args[0]
	if want == "memory" {
		out := fmt.Sprintf("keys:%d,maxkeys:%d", len(keyTypes), client.maxKeys)
		return encodeBulkString(out)
	}
	return ""
}

func cmdAof(client *ClientState, command string) string {
	switch command {
	case "ON":
		client.aofOn = true
		return encodeSimpleString("OK")
	case "OFF":
		client.aofOn = false
		return encodeSimpleString("OK")
	}

	switch command {
	case "DUMP":
		return encodeBulkList(client.aofLog) + encodeSimpleString("OK")
	case "REPLAY":
		for _, line := range client.aofLog {
			args := parseArgs(line)
			client.Dispatch(args[0], args[1:]...)
		}
		return encodeSimpleString("OK")
	case "CLEAR":
		client.aofLog = []string{}
	}
	return ""
}

func cmdSave() string {
	keys := make([]string, 0, len(keyTypes))
	for k := range keyTypes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := []string{}
	for _, k := range keys {
		t := keyTypes[k]
		switch t {
		case "string":
			l := fmt.Sprintf("KEY string %s %s", k, storage[k])
			lines = append(lines, l)
		case "list":
			l := fmt.Sprintf("KEY list %s %s", k, strings.Join(lists[k].Values(), ","))
			lines = append(lines, l)
		case "hash":
			pairs := ""
			for f, v := range hashes[k] {
				if pairs != "" {
					pairs += ","
				}
				pairs += fmt.Sprintf("%s=%s", f, v)
			}
			l := fmt.Sprintf("KEY hash %s %s", k, pairs)
			lines = append(lines, l)
		case "set":
			l := fmt.Sprintf("KEY set %s %s", k, strings.Join(sets[k].Values(), ","))
			lines = append(lines, l)
		}
	}

	out := ""
	for _, l := range lines {
		out += l + "\r\n"
	}
	return out + encodeSimpleString("OK")
}

func cmdRestore(data string) string {
	for _, line := range strings.Split(strings.Trim(strings.Replace(data, "\r\n", "\n", -1), "\n"), "\n") {
		if line == "" {
			continue
		}

		s := strings.Split(line, " ")
		typeName := s[1]
		key := s[2]
		encoded := s[3]

		switch typeName {
		case "string":
			storage[key] = encoded
		case "list":
			list := &List{}
			for _, v := range strings.Split(encoded, ",") {
				list.PushRight(v)
			}
			lists[key] = list
		case "hash":
			hash := make(map[string]string)
			for _, pair := range strings.Split(encoded, ",") {
				kv := strings.Split(pair, "=")
				hash[kv[0]] = kv[1]
			}
			hashes[key] = hash
		case "set":
			set := &Set{}
			for _, v := range strings.Split(encoded, ",") {
				set.Add(v)
			}
			sets[key] = set
		}
	}

	return encodeSimpleString("OK")
}

func cmdRename(source, dest string) string {
	t, found := keyTypes[source]
	if !found {
		return encodeError("ERR no such key")
	}

	switch t {
	case "string":
		oldVal := storage[source]
		delete(storage, source)
		storage[dest] = oldVal
	case "list":
		oldVal := lists[source]
		delete(lists, source)
		lists[dest] = oldVal
	case "set":
		oldVal := sets[source]
		delete(sets, source)
		sets[dest] = oldVal
	case "zset":
		oldVal := zsets[source]
		delete(zsets, source)
		zsets[dest] = oldVal
	case "hashes":
		oldVal := hashes[source]
		delete(hashes, source)
		hashes[dest] = oldVal
	}
	return encodeSimpleString("OK")
}

func cmdSubscribe(channels ...string) string {
	out := []string{}
	for _, ch := range channels {
		mySubscriptions.Add(ch)
		channelSubscriber[ch] = []string{
			"clientId",
		} // maybe this should actully be an actual clientId who requested it
		reply := fmt.Sprintf("subscribe %s %d", ch, mySubscriptions.Len())
		out = append(out, encodeSimpleString(reply))
	}
	return strings.Join(out, "")
}

func cmdPublish(channel, message string) string {
	count := 1
	_, found := channelSubscriber[channel]
	if !found {
		count = 0
	}

	out := []string{}
	if mySubscriptions.IsMember(channel) {
		reply := fmt.Sprintf("message %s %s", channel, message)
		out = append(out, encodeSimpleString(reply))
	}

	out = append(out, encodeInteger(count))
	return strings.Join(out, "")
}

func cmdUnsubscribe(channels ...string) string {
	out := []string{}
	if len(channels) == 0 {
		for key := range mySubscriptions.val {
			channels = append(channels, key)
		}
		sort.Strings(channels)
	}

	for _, ch := range channels {
		mySubscriptions.Remove(ch)
		delete(channelSubscriber, ch)
		reply := fmt.Sprintf("unsubscribe %s %d", ch, mySubscriptions.Len())
		out = append(out, encodeSimpleString(reply))
	}
	return strings.Join(out, "")
}

// TODO not fully done as for now this can only be for '*'
func cmdKeys(keys ...string) string {
	if keys[0] != "*" {
		return encodeError("WRONGTYPE Operation can only be glob '*' pattern now")
	}
	remember := map[string]bool{}
	out := []string{}
	for _, t := range keyTypes {
		if v := remember[t]; v {
			remember[t] = true
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i] < out[j]
	})
	return encodeArray(out)
}

func cmdExists(keys ...string) string {
	out := 0
	for _, k := range keys {
		expiryIfNeeded(k)
		if _, found := storage[k]; found {
			out++
		}
	}
	return encodeInteger(out)
}

func cmdDel(keys ...string) string {
	out := 0
	for _, k := range keys {
		_, found := storage[k]
		expiryIfNeeded(k)
		if found {
			delete(storage, k)
			delete(lists, k)
			delete(hashes, k)
			delete(sets, k)
			delete(zsets, k)
			delete(keyTypes, k)
			delete(expires, k)
			delete(accessTimes, k)
			out++
		}
	}
	return encodeInteger(out)
}

func cmdType(key string) string {
	expiryIfNeeded(key)
	t, found := keyTypes[key]
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
	keyTypes[key] = "string"

	return encodeInteger(sum)
}

func cmdZRank(key, member string) string {
	zset, found := zsets[key]
	if !found {
		return encodeArray(nil)
	}
	out := 0
	for i, m := range zset.GetSorted() {
		if member == m {
			return encodeInteger(i)
		}
		out++
	}
	return encodeNil()
}

func cmdZCard(key string) string {
	zset, found := zsets[key]
	if !found {
		return encodeArray(nil)
	}
	return encodeInteger(zset.Len())
}

func cmdZScore(key, member string) string {
	zset, found := zsets[key]
	if !found {
		return encodeArray(nil)
	}
	s, found := zset.scores[member]
	if !found {
		return encodeArray(nil)
	}
	return encodeBulkString(strconv.FormatFloat(s, 'f', -1, 64))
}

func cmdZRange(args ...string) string {
	key := args[0]
	zset, found := zsets[key]
	if !found {
		return encodeArray(nil)
	}

	start, stop, err := clampRange(args[1], args[2], zset.Len())
	if err != "" {
		return err
	}

	isWithScore := len(args) == 4 && args[3] == "WITHSCORES"

	out := []string{}
	for _, m := range zset.GetSorted() {
		out = append(out, m)
		if isWithScore {
			out = append(out, strconv.FormatFloat(zset.scores[m], 'f', -1, 64))
		}
	}
	return encodeArray(out[start : stop+1])
}

func cmdZAdd(key string, args ...string) string {
	for len(args)%2 != 0 {
		return encodeError("ERR params must be in key-value-pair")
	}

	bucket := make(map[string]float64)
	for i := 0; i < len(args); i += 2 {
		s, err := strconv.ParseFloat(args[i], 64)
		if err != nil {
			return encodeError("ERR params must be in score-value-pair")
		}

		bucket[args[i+1]] = s
	}

	if err := isWrongType(key, "zset"); err != "" {
		return err
	}

	zset, found := zsets[key]
	if !found {
		zset = &ZSet{}
		zsets[key] = zset
		keyTypes[key] = "zset"
	}

	out := 0
	for v, s := range bucket {
		out += zset.Add(s, v)
	}

	return encodeInteger(out)
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
		delete(keyTypes, key)
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

	if err := isWrongType(key, "set"); err != "" {
		return err
	}

	if !found {
		set = &Set{}
		sets[key] = set
		keyTypes[key] = "set"
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
		delete(keyTypes, key)
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
	keyTypes[key] = "string"

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

	list := lists[key]
	if list == nil {
		return encodeArray(nil)
	}

	ln := list.Len()
	start, stop, err := clampRange(args[1], args[2], ln)
	if err != "" {
		return err
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

	if err := isWrongType(key, "list"); err != "" {
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
		delete(keyTypes, key)
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

	if err := isWrongType(key, "list"); err != "" {
		return err
	}

	list, found := lists[key]
	if !found {
		list = &List{}
		lists[key] = list
		keyTypes[key] = "list"
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

type StreamEntry struct {
	id     string
	fields []string
}

var streams = map[string][]StreamEntry{}

func streamIDLess(a, b string) bool {
	am, as := splitStreamID(a)
	bm, bs := splitStreamID(b)
	if am != bm {
		return am < bm
	}
	return as < bs
}

func splitStreamID(id string) (int, int) {
	parts := strings.SplitN(id, "-", 2)
	ms, _ := strconv.Atoi(parts[0])
	seq := 0
	if len(parts) > 1 {
		seq, _ = strconv.Atoi(parts[1])
	}
	return ms, seq
}

func cmdXAdd(key, id string, fields ...string) string {
	if len(fields) == 0 || len(fields)%2 != 0 {
		return encodeError("ERR wrong number of arguments for 'xadd' command")
	}

	if err := isWrongType(key, "stream"); err != "" {
		return err
	}

	entries := streams[key]
	if id == "*" {
		id = fmt.Sprintf("%d-0", len(entries)+1)
	} else if len(entries) > 0 && !streamIDLess(entries[len(entries)-1].id, id) {
		return encodeError("ERR The ID specified in XADD is equal or smaller than the target stream top item")
	}

	streams[key] = append(entries, StreamEntry{id: id, fields: fields})
	keyTypes[key] = "stream"

	return encodeBulkString(id)
}

func cmdXLen(key string) string {
	if err := isWrongType(key, "stream"); err != "" {
		return err
	}
	return encodeInteger(len(streams[key]))
}

func cmdXRange(key, start, stop string) string {
	if err := isWrongType(key, "stream"); err != "" {
		return err
	}

	out := []string{}
	for _, e := range streams[key] {
		if start != "-" && streamIDLess(e.id, start) {
			continue
		}
		if stop != "+" && streamIDLess(stop, e.id) {
			continue
		}
		out = append(out, encodeRawArray([]string{
			encodeBulkString(e.id),
			encodeArray(e.fields),
		}))
	}
	return encodeRawArray(out)
}

func isWrongType(key, want string) string {
	if t, ok := keyTypes[key]; ok && t != want {
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

func clampRange(start string, stop string, ln int) (int, int, string) {
	s1, err := strconv.Atoi(start)
	if err != nil {
		return -1, -1, encodeError("ERR value is not an integer or out of range")
	}

	s2, err := strconv.Atoi(stop)
	if err != nil {
		return -1, -1, encodeError("ERR value is not an integer or out of range")
	}

	if s1 < 0 {
		s1 = ln + s1
	}
	if s2 < 0 {
		s2 = ln + s2
	}
	if s1 < 0 {
		s1 = 0
	}
	if s2 >= ln {
		s2 = ln - 1
	}
	if s1 > s2 {
		return -1, -1, encodeArray(nil)
	}
	return s1, s2, ""
}

func encodeBulkList(items []string) string {
	out := ""
	for _, v := range items {
		out += encodeBulkString(v)
	}
	return out
}

func encodeRawArray(parts []string) string {
	r := fmt.Sprintf("*%d\r\n", len(parts))
	for _, p := range parts {
		r += p
	}
	return r
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
	client := NewClientState()
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		args := parseArgs(line)

		out := client.Dispatch(args[0], args[1:]...)
		fmt.Print(out)

		if contains(WRITE_COMMANDS, args[0]) && !strings.Contains(out, "-") {
			backlog = append(backlog, strings.Join(args, " "))
		}
	}
}
