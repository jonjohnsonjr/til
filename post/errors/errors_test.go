package main

import (
	"errors"
	"io"
	"strconv"
	"testing"
)

type sut func([]item) error

var impls = map[string]sut{
	"bad":  bad,
	"good": good,
}

func BenchmarkCreate(b *testing.B) {
	for _, size := range []int{10, 100, 1_000, 10_000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			items := make([]item, size)
			for name, f := range impls {
				b.Run(name, func(b *testing.B) {
					b.ResetTimer()
					for range b.N {
						f(items)
					}
				})
			}
		})
	}
}

func BenchmarkCheck(b *testing.B) {
	for _, size := range []int{10, 100, 1_000, 10_000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			items := make([]item, size)
			for name, f := range impls {
				b.Run(name, func(b *testing.B) {
					err := f(items)
					b.ResetTimer()
					for range b.N {
						errors.Is(err, io.EOF)
					}
				})
			}
		})
	}
}

type item struct {
	name string
}

var ErrEmptyName = errors.New("no name")

func validate(i item) error {
	if i.name == "" {
		return ErrEmptyName
	}

	return nil
}

func bad(items []item) error {
	var err error

	for _, item := range items {
		err = errors.Join(err, validate(item))
	}

	return err
}

func good(items []item) error {
	var errs []error

	for _, item := range items {
		if err := validate(item); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
