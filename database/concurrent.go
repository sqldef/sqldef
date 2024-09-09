package database

import (
	"sort"

	"golang.org/x/sync/errgroup"
)

type concurrentOutputWithOrdering struct {
	order  int
	output any
}

func ConcurrentMapFuncWithError[Tin any, Tout any](inputs []Tin, concurrency int, f func(Tin) (Tout, error)) ([]Tout, error) {
	eg := errgroup.Group{}
	if concurrency == 0 {
		// disable concurrency
		eg.SetLimit(1)
	} else if concurrency > 0 {
		eg.SetLimit(concurrency)
	} else {
		// no limits
	}

	ch := make(chan concurrentOutputWithOrdering, len(inputs))
	chClosed := false
	defer func() {
		if !chClosed {
			close(ch)
		}
	}()

	for i := range inputs {
		order := i
		in := inputs[i]
		eg.Go(func() error {
			out, err := f(in)
			if err != nil {
				return err
			}
			ch <- concurrentOutputWithOrdering{order, out}
			return err
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	close(ch)
	chClosed = true

	tmp := make([]concurrentOutputWithOrdering, 0, len(inputs))
	for t := range ch {
		tmp = append(tmp, t)
	}

	sort.Slice(tmp, func(i, j int) bool {
		return tmp[i].order < tmp[j].order
	})

	outputs := make([]Tout, 0, len(tmp))
	for _, t := range tmp {
		outputs = append(outputs, t.output.(Tout))
	}

	return outputs, nil
}
