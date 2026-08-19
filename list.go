package main

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
