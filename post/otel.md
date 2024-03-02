# otel

This post describes the idiosyncratic way that I've found to use otel that avoids all the parts that annoy me.

I have a love-hate relationship with otel.
It is immensely useful for debugging concurrency issues and distributed systems, which is why I reach for it often.

Unfortunately, there are a lot of rough edges.
It's difficult to use locally without cobbling together unofficial tools.
At least in the go ecosystem, every version bump seems to break my dependency graph in one way or another.
The go APIs rely on global state, which is convenient for getting started but really limits how you can use it.

Even with the rough edges, it's too useful to ignore, so I use it all the time!

Caveat: I will probably get a lot of stuff wrong here, feel free to send me a PR to correct any misinformation.

## Avoiding Complexity

Recently, my work has been focused on tools, not services.
It can be pretty difficult to figure out what a tool is actually doing, especially if you didn't write it yourself.
The first thing I reach for is [`samply`](./samply.md), but I quickly start adding traces once I hit the limitations of sampling profilers.

## Failed Attempts

You can skip this section if you don't care about existing solutions that didn't work for me.

For tools, I just wanted a way to save and view traces locally.
When I searched for the easiest way to do that, I mostly came across tutorials for setting up Jaeger.
I'm sure Jaeger is amazing, but the [Getting Started](https://www.jaegertracing.io/docs/1.54/getting-started/) page involves running docker and exposing 10 different ports.
This is a little more complicated than I'm comfortable with, so I went looking elsewhere.

The next thing I came across was [`otel-desktop-viewer`](https://github.com/CtrlSpice/otel-desktop-viewer).
Its README has a [Why does this exist?](https://github.com/CtrlSpice/otel-desktop-viewer?tab=readme-ov-file#why-does-this-exist) section that immediately resonated with me.
This is much more straightforward to run; it's a standalone go binary with a nice little browser that tells you what environment variables to export for your app to talk to it.

I have used and loved `otel-desktop-viewer` for a long time, and would recommend at least trying it.
In a lot of ways, it is much better than my current workflow, but it fell short for me in a couple ways that are probably very specific to my use case.

The first "issue" is that you still have to wire up an HTTP or GRPC [exporter](https://opentelemetry.io/docs/languages/go/exporters/) in your application.
This is only really a problem because otel often makes breaking changes that make updating your dependencies difficult.
I was always too reluctant to actually add these deps to tools because I didn't want to condemn any of my coworkers to dependency hell.

My biggest issue is that it requires running a separate process that your app needs to be able to talk to over the network.
Juggling multiple processes and/or making sure the right ports are exposed was enough friction that it would break me out of my flow state.

What I really wanted was a simple way to record the trace data locally and be able to view it later at my leisure.

## Current Solution

### Boilerplate

There is a [`stdout`](https://pkg.go.dev/go.opentelemetry.io/otel/exporters/stdout/stdouttrace) exporter that came to my rescue.
Despite its name, it doesn't always write to stdout, just by default; you can use the `WithWriter` option to write trace data anywhere.

I'm currently adding this stanza wherever I want to capture trace data:

```go
func build(ctx context.Context, traceFile string) error {
    if traceFile != "" {
        // Create the file where we'll write the trace data.
	    w, err := os.Create(traceFile)
	    if err != nil {
		    return fmt.Errorf("creating trace file: %w", err)
	    }
	    defer w.Close()

	    // Initialize our stdout exporter, configured to write to that file.
	    exporter, err := stdouttrace.New(stdouttrace.WithWriter(w))
	    if err != nil {
		    return fmt.Errorf("creating stdout exporter: %w", err)
	    }

	    // Set a global trace provider configured with that exporter.
	    tp := trace.NewTracerProvider(trace.WithBatcher(exporter))
	    otel.SetTracerProvider(tp)

	    defer func() {
	        // Shut down the trace provider (and flush everything, I think?).
		    if err := tp.Shutdown(context.WithoutCancel(ctx)); err != nil {
			    clog.FromContext(ctx).Errorf("shutting down trace provider: %v", err)
		    }
	    }()

        // Create an initial root span that everything will live under.
	    tctx, span := otel.Tracer("melange").Start(ctx, "build")
	    defer span.End()

	    // Overwrite ctx so we propagate the root span through our app.
	    ctx = tctx
    }

    // do normal stuff
}

```

Where `traceFile` comes from an optional `--trace` flag.
If `--trace` isn't set, we don't do any tracing so we can avoid any of the associated overhead.

This example was adapted slightly from [`melange build`](https://github.com/chainguard-dev/melange/blob/0eb18bd438fdd0327060e41cf65bdb59f5ceaf36/pkg/cli/build.go#L89-L111).

Generally, this would be in your `main` function because you only want to do it once, but I'm not bold enough to make `--trace` a global flag anywhere quite yet.

Just doing that isn't enough to get any useful information, since the `--trace` file will just contain a single span.
If you're really lucky, you are using libraries that have already been instrumented with otel spans, so you might actually get useful data for free.
Most of the time, you will have to actually instrument things for this to be useful.

### Instrumentation

Instrumenting a function looks something like this:

```go
import (
    "context"

    "go.opentelemetry.io/otel"
)

func foo(ctx context.Context) error {
    ctx, span := otel.Tracer("my-app").Start(ctx, "foo")
    defer span.End()

    return bar(ctx)
}
```

This will cause `foo` to show up in our trace, so hopefully `foo` is something we care about!
Note that trace data gets propagated via `context.Context`, so if you aren't already plumbing `ctx` around, this is your sign to do the work.
The nice thing is that most functions that we want to instrument _should_ take a `ctx`, which hopefully means they already do :)
After we add a bunch of these to relevant functions in our app, our traces become super useful.

My strategy for picking what functions to instrument is to start with [`samply`](./samply.md) and identify where we're spending most of our CPU time.
You generally don't want to instrument really hot functions that are called thousands of times, since that will generate a ton of spans and slow things down.
You generally _do_, however, want to instrument the functions that contain the loops that call the really hot functions.

As an example, we have a [`LoadImage`](https://github.com/chainguard-dev/melange/blob/0eb18bd438fdd0327060e41cf65bdb59f5ceaf36/pkg/container/bubblewrap_runner.go#L173-L174) method that unpacks a tarball.
We emit a span for `LoadImage`, but we don't emit a span inside that loop for every file.
That would be prety noisy and slow things down for no real reason, so I tend to avoid it unless I'm debugging something really strange.

Once I've got the overall structure of my app instrumented, I look for any gaps where I'm missing an explanation for where my app is spending its time.
Sometimes this is due to HTTP requests ([`otelhttp`](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp) can be useful here).
In a lot of cases, gaps clue me in to some pathological performance issue that we get to fix, which makes everyone happy.
If I see something that seems to take longer than I expected, I'll start digging in and look for ways to parallelize or cache things.

### Visualization

If you're following along, what you're left with is a JSON file that contains trace data.
That isn't particularly useful if you're like me and can't visualize trees in your mind.

While I really love `otel-desktop-viewer`, I wanted something that was easy for me to customize.
I am not a frontend person, and I don't know React, so I wrote a little tool called [`trot`](https://github.com/jonjohnsonjr/trot) that just transforms that JSON data into HTML.
It's really basic and doesn't display a lot of interesting information that is contained in the spans, but it's enough for me.

My workflow now is to run this:

```
melange build --trace ./trace.json && trot < ./trace.json > trace.html && open trace.html
```

No ports or docker containers, it just pops open a browser with my trace data.
I can also easily emit traces anywhere and fetch that file to visualize locally without having to worry about configuration or hosted services, as long as I can persist files somewhere.

## TODO

I'd like to write a tool (probably called `retrace`) that can take `trace.json` and re-export it to whatever exporter you want.
This would allow me to use `otel-desktop-viewer` without modifying most of my workflow.
