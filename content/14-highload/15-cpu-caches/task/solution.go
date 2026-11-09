package task

import "iter"

// Queue — кольцевой буфер на срезе: элементы лежат подряд, head —
// индекс первого, n — число элементов. Ёмкость — степень двойки,
// поэтому индекс по кругу — это & (len-1), а не деление.
type Queue struct {
	buf  []int64
	head int
	n    int
}

func (q *Queue) Push(v int64) {
	if q.n == len(q.buf) {
		q.grow()
	}
	q.buf[(q.head+q.n)&(len(q.buf)-1)] = v
	q.n++
}

// grow удваивает буфер и раскладывает элементы с начала —
// так кольцо, перешедшее через край, снова становится подряд.
func (q *Queue) grow() {
	size := max(8, 2*len(q.buf))
	buf := make([]int64, size)
	for i := range q.n {
		buf[i] = q.buf[(q.head+i)&(len(q.buf)-1)]
	}
	q.buf, q.head = buf, 0
}

func (q *Queue) Pop() (int64, bool) {
	if q.n == 0 {
		return 0, false
	}
	v := q.buf[q.head]
	q.head = (q.head + 1) & (len(q.buf) - 1)
	q.n--
	return v, true
}

func (q *Queue) Peek() (int64, bool) {
	if q.n == 0 {
		return 0, false
	}
	return q.buf[q.head], true
}

func (q *Queue) Len() int { return q.n }

func (q *Queue) All() iter.Seq[int64] {
	return func(yield func(int64) bool) {
		for i := range q.n {
			if !yield(q.buf[(q.head+i)&(len(q.buf)-1)]) {
				return
			}
		}
	}
}
