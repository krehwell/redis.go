# redis-go

A Redis server written from scratch in Go, one lesson at a time.

It reads commands from stdin and writes RESP replies to stdout. No networking, no dependencies, just the standard library.

Built by following [Build Redis from Scratch](https://shipthatcode.com/courses/build-redis) on shipthatcode.

[![shipthatcode — Build Redis from Scratch](https://api.shipthatcode.com/cert/cbbcdb1adb9520c7966f3fd5cae2eba2.svg)](https://shipthatcode.com/courses/build-redis)

## Run it

```sh
go run main.go
```

Then type commands:

```
SET name alice
GET name
LPUSH mylist a b c
LRANGE mylist 0 -1
```

Or feed it a test file:

```sh
go run main.go < tests/13-lpush-rpush/1.in
```

## Test

```sh
./run_tests.sh 13     # one lesson
./run_tests.sh        # everything
```

To see a single test next to what it should be:

```sh
go run main.go < tests/13-lpush-rpush/1.in | diff - tests/13-lpush-rpush/1.out
```

## What works

Lessons 1 to 26. Strings, lists, hashes, sets, sorted sets, expiry, transactions, pub/sub, persistence, eviction.

```
PING ECHO COMMAND
SET GET DEL EXISTS KEYS TYPE RENAME DBSIZE
INCR DECR INCRBY DECRBY
EXPIRE TTL PTTL PERSIST
LPUSH RPUSH LPOP RPOP LLEN LRANGE
HSET HGET HGETALL HEXISTS HDEL HLEN
SADD SCARD SISMEMBER SREM
ZADD ZRANGE ZSCORE ZCARD ZRANK
MULTI EXEC DISCARD WATCH UNWATCH
SUBSCRIBE UNSUBSCRIBE PUBLISH
SAVE RESTORE AOF MAXKEYS INFO
```

Still to do: `EVAL` (Lua), replication, streams.

## Notes

**Everything goes in `main.go`.** The grader compiles that one file, nothing else. Splitting into packages breaks it.

**Lesson 5 fails on purpose.** It sends RESP arrays, lessons 6 onward send plain lines. One parser cannot read both. The old parser is at commit `d0d2585`.

**`WAIT` is fake.** It moves a pretend clock forward so expiry tests do not have to sleep. Anything reading the time calls `now()`.
