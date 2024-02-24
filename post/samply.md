# samply

I like [`samply`](https://github.com/mstange/samply).
I like `samply` _a lot_.
It's the first tool I reach for when debugging a slow program.
Most of the time, it's also the last tool, because it's so very good.

I used to use go's pprof tooling to generate flamegraphs, but I stopped doing that once I discovered `samply`.

There are two major reasons I reach for `samply` by default now:

## 1. It just works

The go pprof tooling is really flexible and can give you a ton of very useful data.
If you're profiling a test, it just works by default because `go test` supports various flags like `-cpuprofile`.
If you're running an HTTP server, go is also great because you can register a pprof debug handler in one line of code:

```go
import _ "net/http/pprof"
```

However, if you want to just debug an existing program and don't want to have to recompile it to get profile data, you're out of luck.
There is a [short little stanza](https://pkg.go.dev/runtime/pprof#hdr-Profiling_a_Go_program) you can add to your main function, which is nice, but that's just enough friction to pull me out of my "flow" state.
I even tried to hack around this by writing a little library called [`mane`](https://github.com/jonjohnsonjr/mane/blob/main/README.md#pprof) to do it for me.
Unfortunately, there's was still enough friction around calling `go get` and `go mod tidy` that it would disrupt my train of thought regardless.

Contrast that with just prepending `samply record` to your command.
You don't have to add any code.
You don't have to recompile your binary.
You just type two words!

## 2. The timeline view

Having a single flamegraph is great, but it doesn't show you _when_ things are being executed.
In order to contextualize the flamegraph, I'd have to instrument the binary with otel spans as well.
This allows you to visualize CPU usage over time (and across threads) which can show you where you might be able to parallelize things.
Even more useful is the ability to select individual threads and slices of time in the profile.
This gives you essentially a poor man's trace view, because you can drill down into sections of the profile and infer what's going on from the flamegraphs.

## Caveats

One place I find `samply` lacking is in debugging things that aren't CPU-intensive, e.g. network calls.
That's not really `samply`'s fault -- it just doesn't work that well for things that wait for slow I/O.
For that, I still reach for otel spans, which is a TIL for another day.

Something else worth mentioning is that `samply` only really works for certain kinds of programs.
I write a lot of go code, which means `samply` works well for me.
Go programs are usually compiled on my own machine, which means MacOS doesn't yell at me when I try to trace them.
They are also statically compiled, which means the stacks have useful symbols, unlike e.g. python programs.

I did recently use [`py-spy`](https://github.com/benfred/py-spy) for profiling python programs, if that's your kind of thing.
Also, Julia Evans has a bunch of posts about [profilers](https://jvns.ca/categories/ruby-profiler/) and [`rbspy`](https://jvns.ca/juliasections/rbspy/) that explain how profilers work and how she built one.
I highly recommend reading everything she has ever written.

