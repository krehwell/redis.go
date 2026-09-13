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

When the output looks identical but the test still fails, it is line endings. RESP wants `\r\n` everywhere:

```sh
go run main.go < tests/13-lpush-rpush/1.in | xxd | tail
```

`0d 0a` is right, a bare `0a` is not.

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

**Everything lives in `main.go`.** The grader compiles that one file and nothing else, so splitting into packages breaks it. See `entrypoint` in `.shipthatcode.json`.

**Lesson 5 fails on purpose.** It feeds RESP arrays (`*2\r\n$3\r\nGET\r\n...`) while lessons 6 onward feed plain lines (`GET foo`). One parser cannot do both. The course says each lesson is its own exercise; the lesson 5 parser is in git history at `d0d2585`.

**`WAIT` is not a real Redis command here.** It moves a fake clock forward so expiry can be tested without the suite sleeping for 6 seconds. Everything that reads the time goes through `now()`, which is `time.Now()` plus that offset.
