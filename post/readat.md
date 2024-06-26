# ReadAt

For a long while, I've been confused about something in go's standard library.
There's [`io.ReaderAt`](https://pkg.go.dev/io#ReaderAt), [`io.ReadSeeker`](https://pkg.go.dev/io#ReadSeeker), and [`io.SectionReader`](https://pkg.go.dev/io#SectionReader).
Some APIs expect an `io.ReaderAt`, but others take an `io.ReadSeeker`.
Why do we have all these things?
How do they relate to each other?
When do I use which thing for what?

I recently had a moment of clarity surrounding all of this, so I'm writing it down before I forget it.

## State Can Ruin Everything Around Me

### tl;dr

Let's start with the core idea, which will double as a tl;dr if you are smarter than I am:

The `io.ReaderAt` interface allows for a **stateless** implementation, whereas `io.Reader` and `io.Seeker` are inherently stateful.

Even though `io.ReaderAt` and `io.ReadSeeker` are kind of interchangeable, `io.ReaderAt` is a more powerful interface because of this statelessness.

### `io.ReadSeeker` vs `io.ReaderAt`

To expand on what I mean by these being interchangeable:

Given an `io.ReaderAt`, you can easily implement an `io.ReadSeeker`.
To `Seek`, you just update a cursor for the current position.
To `Read`, you just `ReadAt` that current position, then move the cursor forward.
Alternatively, you can just use `io.SectionReader`, which will do this for you.

Given an `io.ReadSeeker`, you can [easily](https://cs.opensource.google/go/go/+/master:src/go/internal/gccgoimporter/ar.go;l=152-171;drc=13159fef0423fe908aac676d7c4f377c2ae41f49) implement an `io.ReaderAt` by `Seek`ing to the offset and calling `Read`.

So what's the difference?
Does it matter which one we use?

Just looking at the interace, an `io.ReadSeeker` precludes having multiple concurrent consumers.
If two readers both `Seek` at the same time, one of them will end up calling `Read` from the wrong offset.
You can of course throw a mutex around the `Seek` + `Read` calls to make it safe, but then you're just serializing everything.

Compare that to `io.ReaderAt` which atomically reads some number of bytes from an offset.
This interface makes it at least _possible_ to have multiple concurrent callers in a useful way.

To summarize: `io.ReaderAt`'s statelessness allows concurrent consumers without synchronization.

### Why `io.SectionReader`, then?

If `io.ReaderAt` is more powerful than `io.ReadSeeker`, what's the point of `io.SectionReader`?

Well... while `io.ReaderAt` is more _powerful_, it's way less _ergonomic_.

The go standard library is largely built around `io.Reader` and `io.Writer` abstractions.
While `io.ReaderAt` is more powerful in theory, in practice you need some way to turn it into an `io.Reader` to pass into e.g. `io.Copy`.
You could write a fairly simple wrapper to keep track of the current offset, but you can also just use `io.SectionReader`!

```go
r := io.NewSectionReader(ra, 0, size)
io.Copy(w, r)
```

The "section" here ends up being the entire range of bytes, which feels a little odd, but you get a free `io.Reader` implementation from an `io.ReaderAt`.
This lets you bridge an `io.ReaderAt` into any API that expects an `io.Reader`.

### Why `io.ReaderAt`, then?

Okay, sure, fair.
There's not a ton of packages in the standard library that takes advantage of an `io.ReaderAt`.
The only ones I've really used are:

* archive/zip
* debug/elf

These are file formats that deal a lot with offsets to binary data.

Because `zip.NewReader` takes an `io.ReaderAt` instead of an `io.ReadSeeker`, we can safely open and read multiple files in the zip archive without worrying about any synchronization.

However, just because the standard library doesn't use `io.ReaderAt` much doesn't mean that you can't!
The whole point of this point is to encourage you to exploit the power of `io.ReaderAt` in your own code.

### Why `io.ReadSeeker`, then?

So if `io.ReaderAt` and `io.ReadSeeker` are both roughly equivalent, and `io.ReaderAt` allows for safe concurrency, why might an API accept an `io.ReadSeeker` over an `io.ReaderAt`?
I've noticed a few patterns.

`http.ServeContent` [seeks](https://cs.opensource.google/go/go/+/refs/tags/go1.22.2:src/net/http/fs.go;l=195-205;drc=1d45a7ef560a76318ed59dfdb178cecd58caf948) to the end of an `io.ReadSeeker` to determine its size.
This feels like a pretty janky use of `Seek`, but since there's not any `io.Sizer` interface, I get it.

`http.ServeContent` also [seeks](https://cs.opensource.google/go/go/+/refs/tags/go1.22.2:src/net/http/fs.go;l=239-247;drc=1d45a7ef560a76318ed59dfdb178cecd58caf948) back to the start after "sniffing" the first 512 bytes of data to try to automatically a `Content-Type`.
Personally, I think it would be cleaner to `Peek` those bytes, but I assume that this was done because we already needed an `io.Seeker` for the size hack and to avoid an additional allocation.

I guess my assertion at the beginning that `io.ReaderAt` is more powerful is not universally true.
You can't (cheaply) use an `io.ReaderAt` to determine the size of something.
I don't think this is a great use of `io.Seeker`, but it's used in the standard library, so what do I know?
(Maybe I do know things, given https://github.com/golang/go/issues/25854.)

Aside: Xe has an [excellent post](https://xeiaso.net/blog/2024/fixing-rss-mailcap/) about `http.ServeContent` doing this, and the unfortunate consequences.

FWIW, if you know the size and are willing to buffer 512 bytes, I have dealt with this existential torment in the past using something like this:

<details><summary>(click to expand)</summary>

```go
// golang/src/net/http/sniff.go
const sniffLen = 512

type sizeAndSniff struct {
	r      io.Reader
	size   int64
	buf    *bufio.Reader
	seeked bool
}

func (s *sizeAndSniff) Seek(offset int64, whence int) (int64, error) {
	s.seeked = true

	// Checking the size.
	if offset == 0 && whence == io.SeekEnd {
		return s.size, nil
	}

	// Resetting.
	if offset == 0 && whence == io.SeekStart {
		return 0, nil
	}

	// Anything else is unexpected.
	return 0, fmt.Errorf("unexpected seek(%d, %d) for sizeAndSniff", offset, whence)
}

func (s *sizeAndSniff) Read(p []byte) (int, error) {
	// Handle first read.
	if s.buf == nil {
		if len(p) <= sniffLen {
			s.buf = bufio.NewReaderSize(s.r, sniffLen)
		} else {
			s.buf = bufio.NewReaderSize(s.r, len(p))
		}

		// Currently, http.ServeContent will sniff before it seeks for size.
		// If we haven't seen a Read() but have seen a Seek already, that means we shouldn't peek.
		if !s.seeked {
			// Peek to handle the first content sniff.
			b, err := s.buf.Peek(len(p))
			if err != nil {
				if err == io.EOF {
					n, _ := bytes.NewReader(b).Read(p)
					return n, io.EOF
				} else {
					return 0, err
				}
			}
			return bytes.NewReader(b).Read(p)
		}
	}

	// This assumes they will always sniff then reset.
	n, err := s.buf.Read(p)
	return n, err
}
```

</details>

## Applications

### `targz`

This post was inspired by some work I'm been tinkering with over in https://github.com/jonjohnsonjr/targz.
I'll have more to say about that once it's a little more fleshed out, but it's usable enough to demonstrate some of the concepts I've been talking about.

## Implications

### CLI Design

When I first realized how nice `io.ReaderAt` was, I actually felt a little sad.
My standard CLI design is to read from stdin and write to stdout.
I usually use [cobra](https://github.com/spf13/cobra), which means I can use the `InOrStdin` and `OutOrStdout` helpers to make it easier to write tests.
These helpers return an `io.Reader` and `io.Writer`, respectively, which means I can't use these and `io.ReaderAt`.

Moreover, I can't just read from stdin and write to stdout -- instead, I need to take a filename as a flag or argument, which makes me feel a little unclean.

E.g. instead of this:

```console
$ mytool < input > output
```

I'd have to write this:

```console
$ mytool input output
```

I don't like that!
But then I had another realization: this was only true because I had not yet achieved sufficient levels of pedantry.

### Harmful Use Of Cat

I've come across the ["Useless Use Of Cat"](https://en.wikipedia.org/wiki/Cat_(Unix)#Useless_use_of_cat) internet meme a few times in my life, and I've always dismissed it because, really, who cares?

To summarize, internet pedants will often point out that `cat file | cmd` is less efficient than `cmd < file` because the former creates an extra process and pipe.
I've always thought this was a silly thing to care about.
A process and a pipe are cheap, and I find it easier to reason about catting the file as starting the pipeline than having `< file` come _after_ the command.

Two things have changed my mind:

1. This syntax also works: `< file cmd`, which is actually shorter than `cat file | cmd`.
2. If you want to support `io.ReaderAt` and read from stdin, you can achieve that as long as you don't have a useless use of cat!

In fact, the Wikipedia page even mentions this:

> Beyond other benefits, the input redirection forms allow command to perform random access on the file, whereas the cat examples do not. This is because the redirection form opens the file as the stdin file descriptor which command can fully access, while the cat form simply provides the data as a stream of bytes.

I'd just never read the Wikipedia page until I went looking for something to link to for this article.
It was there the whole time!

I think one reason this doesn't come up is that the useless use of cat primary source is from circa 2000, when concurrent programming wasn't quite as common as it is today.

Aside: I thought I was really clever for coming up with "harmful use of cat", but as of this writing Google surfaces exactly [one result](https://github.com/ansible/ansible/issues/12459#issuecomment-282607588) from 2017.
I am slightly disappointed by this, but it's also really beautiful that I can feel connected to a stranger on the internet who had the same clever thought while helping yet another internet stranger over 7 years ago.

Anyway, since `os.Stdin` and `os.Stdout` are both instances of `os.File`, you can (sometimes, see below) use them as an `io.ReaderAt` or `io.WriterAt`!

## Caveats

### Go Is A Liar Sometimes

An example in [`tar.Writer`](https://cs.opensource.google/go/go/+/master:src/archive/tar/writer.go;l=613-622;drc=22344034c547da2e656e2a63a69b555ee974d1a8):

```go
func (sw *sparseFileWriter) ReadFrom(r io.Reader) (n int64, err error) {
	rs, ok := r.(io.ReadSeeker)
	if ok {
		if _, err := rs.Seek(0, io.SeekCurrent); err != nil {
			ok = false // Not all io.Seeker can really seek
		}
	}
	if !ok {
		return io.Copy(struct{ io.Writer }{sw}, r)
	}
	⋮
```

This looks pretty gross to me!

For our use case, `os.Stdin` is an `os.File`, which means it implements `io.ReaderAt`, right?
Well, most of the time, no!

If you're reading from a pipe, it will certainly not work!
You'll get an error like this:

```
read /dev/stdin: illegal seek
```

We can determine if `os.Stdin` actually implements `io.ReaderAt` but attempting to seeK:

```go
if _, err := os.Stdin.Seek(0, io.SeekCurrent); err != nil {
    // DON'T BELIEVE HIS LIES
}
```

If we're reading from a pipe, we'll get an error like this:

```
seek /dev/stdin: illegal seek
```

If you are lazy like me and don't want to implement an API that supports both `io.ReaderAt` and `io.Reader`, you can just copy `os.Stdin` to a temporary file if it's a pipe:

```go
f := os.Stdin

if _, err := f.Seek(0, io.SeekCurrent); err != nil {
    tmp, err := os.CreateTemp("", "")
    if err != nil {
        return err
    }
    defer os.Remove(tmp.Name())

    if _, err := os.Copy(tmp, f); err != nil {
        return err
    }

    if _, err := tmp.Seek(0, io.SeekStart); err != nil {
        return err
    }

    f = tmp
}

// Now you can always use f as an io.ReaderAt.
```

### Where's Size?

If you do write an API that accepts an `io.ReaderAt`, you should also accept a size argument like `zip.NewReader` because you will need it for `io.SectionReader`.
Similarly, if you implement the `io.ReaderAt` interface, you should also expose the size of the underlying data so that your users can use it effectively.

See this unfortunate pair of issues for more context:

* https://github.com/golang/go/issues/15818
* https://github.com/golang/go/issues/15822

In most cases, you can just call [`Stat`](https://pkg.go.dev/os#Stat), [`Stat`](https://pkg.go.dev/os#File.Stat), [`Stat`](https://pkg.go.dev/io/fs#Stat), or [`Stat`](https://pkg.go.dev/io/fs#File.Stat) to get the size.
Oh and don't forget about [`ContentLength`](https://pkg.go.dev/net/http#Response.ContentLength) to get the size of a different computer's files.

## Even More Reading

* While writing this, I came across https://fasterthanli.me/articles/abstracting-away-correctness, which covers a lot of the same ground but with much higher production value.
* I also came across https://www.doxsey.net/blog/fixing-interface-erasure-in-go/, which goes into more depth about problems with interfaces in go.
