# samply

I like [`samply`](https://github.com/mstange/samply).
I like `samply` _a lot_.
It's the first tool I reach for when debugging a slow program.
Most of the time, it's also the last tool, because it's so very good.

I used to use go's pprof tooling to generate flamegraphs, but I stopped doing that once I discovered `samply`.
Having a single flamegraph is great, but it doesn't show you _when_ things are being executed.
In order to contextualize the flamegraph, I'd have to instrument the binary with otel spans as well.

The killer feature that made me switch to `samply` is the timeline view.
This allows you to visualize CPU usage over time (and across threads) which can show you where you might be able to parallelize things.
Even more useful is the ability to select individual threads and slices of time in the profile.
This gives you essentially a poor man's trace view, because you can drill down into sections of the profile and infer what's going on from the flamegraphs.

The one place I find `samply` lacking is in debugging things that aren't CPU-intensive, e.g. network calls.
For that I still reach for otel spans, which is a TIL for another day.
