//go:build ignore
// +build ignore

package main

// To support a new type in this package, one can add types to `codes`,
// then run: `go generate ./...` in this folder, to generate more desired
// concrete typed unbounded channels or queues.
//
// Note that chan_struct.go is a specialized implementation for struct{}
// objects. If one changes the code template, then those changes should
// also be synced to chan_struct.go file manually.

import (
	"bytes"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"text/template"
)

type data struct {
	Type    string
	Name    string
	Imports string
}

func main() {
	codes := map[*template.Template]map[string]data{
		chanImpl: {
			"chan_canvasobject.go": {
				Type:    "fyne.CanvasObject",
				Name:    "CanvasObject",
				Imports: `import "fyne.io/fyne/v2"`,
			},
			"chan_func.go": {
				Type:    "func()",
				Name:    "Func",
				Imports: "",
			},
			"chan_interface.go": {
				Type:    "interface{}",
				Name:    "Interface",
				Imports: "",
			},
		},
		queueImpl: {
			"queue_canvasobject.go": {
				Type: "fyne.CanvasObject",
				Name: "CanvasObject",
				Imports: `import (
					"sync"
					"sync/atomic"

					"fyne.io/fyne/v2"
				)`,
			},
		},
		queueUnsafeStructImpl: {
			"queue_unsafe_canvasobject.go": {
				Type: "fyne.CanvasObject",
				Name: "CanvasObject",
				Imports: `import (
					"sync/atomic"
					"unsafe"

					"fyne.io/fyne/v2"
				)`,
			},
		},
	}

	for tmpl, types := range codes {
		for fname, data := range types {
			buf := &bytes.Buffer{}
			err := tmpl.Execute(buf, data)
			if err != nil {
				panic(fmt.Errorf("failed to generate %s for type %s: %v", tmpl.Name(), data.Type, err))
			}

			code, err := format.Source(buf.Bytes())
			if err != nil {
				panic(fmt.Errorf("failed to format the generated code:\n%v", err))
			}

			os.WriteFile(fname, code, fs.ModePerm)
		}
	}
}

var chanImpl = template.Must(template.New("async").Parse(`// Code generated by go run gen.go; DO NOT EDIT.

package async

{{.Imports}}

// Unbounded{{.Name}}Chan is a channel with an unbounded buffer for caching
// {{.Name}} objects. A channel must be closed via Close method.
type Unbounded{{.Name}}Chan struct {
	in, out chan {{.Type}}
	close   chan struct{}
	q       []{{.Type}}
}

// NewUnbounded{{.Name}}Chan returns a unbounded channel with unlimited capacity.
func NewUnbounded{{.Name}}Chan() *Unbounded{{.Name}}Chan {
	ch := &Unbounded{{.Name}}Chan{
		// The size of {{.Name}} is less than 16 bytes, we use 16 to fit
		// a CPU cache line (L2, 256 Bytes), which may reduce cache misses.
		in:  make(chan {{.Type}}, 16),
		out: make(chan {{.Type}}, 16),
		close: make(chan struct{}),
	}
	go ch.processing()
	return ch
}

// In returns the send channel of the given channel, which can be used to
// send values to the channel.
func (ch *Unbounded{{.Name}}Chan) In() chan<- {{.Type}} { return ch.in }

// Out returns the receive channel of the given channel, which can be used
// to receive values from the channel.
func (ch *Unbounded{{.Name}}Chan) Out() <-chan {{.Type}} { return ch.out }

// Close closes the channel.
func (ch *Unbounded{{.Name}}Chan) Close() { ch.close <- struct{}{} }

func (ch *Unbounded{{.Name}}Chan) processing() {
	// This is a preallocation of the internal unbounded buffer.
	// The size is randomly picked. But if one changes the size, the
	// reallocation size at the subsequent for loop should also be
	// changed too. Furthermore, there is no memory leak since the
	// queue is garbage collected.
	ch.q = make([]{{.Type}}, 0, 1<<10)
	for {
		select {
		case e, ok := <-ch.in:
			if !ok {
				// We don't want the input channel be accidentally closed
				// via close() instead of Close(). If that happens, it is
				// a misuse, do a panic as warning.
				panic("async: misuse of unbounded channel, In() was closed")
			}
			ch.q = append(ch.q, e)
		case <-ch.close:
			ch.closed()
			return
		}
		for len(ch.q) > 0 {
			select {
			case ch.out <- ch.q[0]:
				ch.q[0] = nil // de-reference earlier to help GC
				ch.q = ch.q[1:]
			case e, ok := <-ch.in:
				if !ok {
					// We don't want the input channel be accidentally closed
					// via close() instead of Close(). If that happens, it is
					// a misuse, do a panic as warning.
					panic("async: misuse of unbounded channel, In() was closed")
				}
				ch.q = append(ch.q, e)
			case <-ch.close:
				ch.closed()
				return
			}
		}
		// If the remaining capacity is too small, we prefer to
		// reallocate the entire buffer.
		if cap(ch.q) < 1<<5 {
			ch.q = make([]{{.Type}}, 0, 1<<10)
		}
	}
}

func (ch *Unbounded{{.Name}}Chan) closed() {
	close(ch.in)
	for e := range ch.in {
		ch.q = append(ch.q, e)
	}
	for len(ch.q) > 0 {
		select {
		case ch.out <- ch.q[0]:
			ch.q[0] = nil // de-reference earlier to help GC
			ch.q = ch.q[1:]
		default:
		}
	}
	close(ch.out)
	close(ch.close)
}
`))

var queueUnsafeStructImpl = template.Must(template.New("queue").Parse(`// Code generated by go run gen.go; DO NOT EDIT.
//go:build !js
// +build !js

package async

{{.Imports}}

// {{.Name}}Queue implements lock-free FIFO freelist based queue.
//
// Reference: https://dl.acm.org/citation.cfm?doid=248052.248106
type {{.Name}}Queue struct {
	head unsafe.Pointer
	tail unsafe.Pointer
	len  uint64
}

// New{{.Name}}Queue returns a queue for caching values.
func New{{.Name}}Queue() *{{.Name}}Queue {
	head := &item{{.Name}}{next: nil, v: nil}
	return &{{.Name}}Queue{
		tail: unsafe.Pointer(head),
		head: unsafe.Pointer(head),
	}
}

type item{{.Name}} struct {
	next unsafe.Pointer
	v    {{.Type}}
}

func load{{.Name}}Item(p *unsafe.Pointer) *item{{.Name}} {
	return (*item{{.Name}})(atomic.LoadPointer(p))
}

func cas{{.Name}}Item(p *unsafe.Pointer, old, new *item{{.Name}}) bool {
	return atomic.CompareAndSwapPointer(p, unsafe.Pointer(old), unsafe.Pointer(new))
}
`))

var queueImpl = template.Must(template.New("queue").Parse(`// Code generated by go run gen.go; DO NOT EDIT.

package async

{{.Imports}}

var item{{.Name}}Pool = sync.Pool{
	New: func() interface{} { return &item{{.Name}}{next: nil, v: nil} },
}

// In puts the given value at the tail of the queue.
func (q *{{.Name}}Queue) In(v {{.Type}}) {
	i := item{{.Name}}Pool.Get().(*item{{.Name}})
	i.next = nil
	i.v = v

	var last, lastnext *item{{.Name}}
	for {
		last = load{{.Name}}Item(&q.tail)
		lastnext = load{{.Name}}Item(&last.next)
		if load{{.Name}}Item(&q.tail) == last {
			if lastnext == nil {
				if cas{{.Name}}Item(&last.next, lastnext, i) {
					cas{{.Name}}Item(&q.tail, last, i)
					atomic.AddUint64(&q.len, 1)
					return
				}
			} else {
				cas{{.Name}}Item(&q.tail, last, lastnext)
			}
		}
	}
}

// Out removes and returns the value at the head of the queue.
// It returns nil if the queue is empty.
func (q *{{.Name}}Queue) Out() {{.Type}} {
	var first, last, firstnext *item{{.Name}}
	for {
		first = load{{.Name}}Item(&q.head)
		last = load{{.Name}}Item(&q.tail)
		firstnext = load{{.Name}}Item(&first.next)
		if first == load{{.Name}}Item(&q.head) {
			if first == last {
				if firstnext == nil {
					return nil
				}
				cas{{.Name}}Item(&q.tail, last, firstnext)
			} else {
				v := firstnext.v
				if cas{{.Name}}Item(&q.head, first, firstnext) {
					atomic.AddUint64(&q.len, ^uint64(0))
					item{{.Name}}Pool.Put(first)
					return v
				}
			}
		}
	}
}

// Len returns the length of the queue.
func (q *{{.Name}}Queue) Len() uint64 {
	return atomic.LoadUint64(&q.len)
}
`))
