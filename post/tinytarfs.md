## Tiny TarFS

[Previously](tarfs.md), we looked at `tarfs`.

I'm not quite done with that, but I found a rabbit hole and wanted to jot it down before I find another.

In `tarfs`, I mentioned:

> In theory, we could store only the offsets to the header data in the original tar file so that we don't store it twice, but by duplicating the header data, we can render the entire filesystem from just the index without actually having access to the original tar file.
> This technique is useful to me for quickly rendering results on dag.dev, since I can just store the indexes locally and render the filesystem view without hitting the registry.

Let's expand on that a bit.


### Smaller Index Variant

In looking for other implementations, I came across [nlepage/go-tarfs](https://github.com/nlepage/go-tarfs).
It's new enough to have the right API (using `io/fs`), but it takes a different approach than mine in an interesting way.
Instead of storing the offset to the file contents, it stores [the offset - 512](https://github.com/nlepage/go-tarfs/blob/7b1a8fac13d9033f2b1fdad64486c19ad8803cb2/fs.go#L70).
Initially I thought this was to avoid storing the header data, but it turns out that gets stored anyway [because of this call](https://github.com/nlepage/go-tarfs/blob/7b1a8fac13d9033f2b1fdad64486c19ad8803cb2/fs.go#L65), but my misunderstanding actually gave me an interesting idea.

Imagine you want to optimize for having a very small tarfs index.
Doing what I do (storing all the `tar.Header` info) is useful for me, but is actually more than you need to satisfy the [`fs.ReadDirFS`](https://pkg.go.dev/io/fs#ReadDirFS) interface.
If you're willing tolerate more expensive calls to [`fs.DirEntry.Info()`](https://pkg.go.dev/io/fs#DirEntry.Info) in exchange for a smaller index, you could certainly store less.
Let's look at how you might do that.

In order to implement `ReadDir`:

```
func ReadDir(fsys FS, name string) ([]DirEntry, error)
```

We need to satisfy `DirEntry`:

```go
type DirEntry interface {
	// Name returns the name of the file (or subdirectory) described by the entry.
	// This name is only the final element of the path (the base name), not the entire path.
	// For example, Name would return "hello.go" not "home/gopher/hello.go".
	Name() string

	// IsDir reports whether the entry describes a directory.
	IsDir() bool

	// Type returns the type bits for the entry.
	// The type bits are a subset of the usual FileMode bits, those returned by the FileMode.Type method.
	Type() FileMode

	// Info returns the FileInfo for the file or subdirectory described by the entry.
	// The returned FileInfo may be from the time of the original directory read
	// or from the time of the call to Info. If the file has been removed or renamed
	// since the directory read, Info may return an error satisfying errors.Is(err, ErrNotExist).
	// If the entry denotes a symbolic link, Info reports the information about the link itself,
	// not the link's target.
	Info() (FileInfo, error)
}
```

We already need `Name()` to implement `Open`, so that shouldn't be hard, but we also keep the file offset around to make `Open` fast.

Looking at `IsDir()` and `Type()`, we need to support the `FileMode` bits, and it turns out there are 7 relevant bits:

```go
// Mask for the type bits. For regular files, none will be set.
ModeType = ModeDir | ModeSymlink | ModeNamedPipe | ModeSocket | ModeDevice | ModeCharDevice | ModeIrregular
```

Realistically, some of these bits are mutually exclusive, but let's be a little bit lazy and assume we want to store these 7 bits as 7 bits, for convenience.
My initial idea was to use [roaring bitmaps](https://roaringbitmap.org/), but then [`@rogpeppe`](https://github.com/rogpeppe) pointed out that there's a cute trick we could do without any external dependencies or complicated data structures.

For each file, we're already storing the file offset as an int64 so we can jump to it.
Notably, all of these file offsets are going to be multiples of 512 because tar files are aligned to 512 byte boundaries.
This means that the last 9 bits of the file offset are always going to be zero, and we don't actually need to look at those bits, so we're free to do whatever we want with them.

Picking a random multiple of 512, as an int64 this looks something like:

```
0000000000000000000000000000000000000000000000001001101000000000
                                                       ^^^^^^^^^
                                                       these will always be zero
```

(Let's ignore all those zeroes at the front for now.)

So imagine that we actually use those bits e.g. to store the 7 bits of the FileMode.
We can't do that directly, unfortunately, because:

```go
// 10001111001010000000000000000000
fmt.Printf("%b", fs.ModeType)
```

But we could do some bit twiddling to just move all of those bits together.
Then you can imagine that a directory entry might look something like:

```
0000000000000000000000000000000000000000000000001001101000000001
                                                               ^
                                                       this means it's a dir
```

As it turns out, `tar.Header.Typeflag` is kind of this already.
It's a single byte, but it only uses 7 of the bits, so we can just stuff it in there and use the same logic the `tar` package uses to convert to `FileMode` bits.

```go
// 00110000
// 00000000
// 00110001
// 00110010
// 00110011
// 00110100
// 00110101
// 00110110
// 00110111
// 01111000
// 01100111
// 01010011
// 01001100
// 01001011
for _, flag := range []byte{
	'0',    // TypeReg
	'\x00', // TypeRegA
	'1',    // TypeLink
	'2',    // TypeSymlink
	'3',    // TypeChar
	'4',    // TypeBlock
	'5',    // TypeDir
	'6',    // TypeFifo
	'7',    // TypeCont
	'x',    // TypeXHeader
	'g',    // TypeXGLobalHeader
	'S',    // TypeGNUSparse
	'L',    // TypeGNULongName
	'K',    // TypeGnuLongLink
} {
    fmt.Printf("%0b\n", flag)
}
```

This also gets us more than just the `ModeType` bits because most of those are actually mutually exclusive and spending a whole bit on each is wasteful.
So, what do we want to do with those two bits?

The other fairly wasteful thing we're storing are the filenames.
Let's look at a go APK that I happen to have lying around (omitting uninteresting parts):

```
tar -tf ~/testdata/go.apk
usr
usr/bin
usr/bin/go
usr/bin/gofmt
usr/lib
usr/lib/go/pkg/tool
var
```

You might notice that there are a lot of repeated prefixes.
This is something that DEFLATE is usually good at compressing, but I have a theory that we can put those two bits to good use to do b fietter than DEFLATE on its own.

There are ~four kinds of transitions I see between lines:

1. `usr/lib/go/pkg/tool` -> `var`, we aren't reusing any of the previous file, so we "reset" to root-relative string.
2. `usr` -> `usr/bin`, we can just `path.Join(prev, "bin")` to produce the next line.
3. `usr/bin/go` -> `usr/bin/gofmt`, we can just `path.Join(path.Dir(prev), "bin")` to produce the next line.
4. `usr/bin/gofmt` -> `usr/lib`, we can just `path.Join(path.Dir(path.Dir(prev)), "lib")` to produce the next line.

With the two spare bits we have, instead of this: 

```
usr
usr/bin
usr/bin/go
usr/bin/gofmt
usr/lib
usr/lib/go/pkg/tool
var
```

We could encode it as:

```
(00) usr
(01) bin
(01) go
(10) gofmt
(11) lib
(01) go/pkg/tool
(00) var
```

And since those bits are ~free to us (stored in the parallel under the offsets), we've reduced the size of the names in the index by quite a bit.

The final piece is implementing `Info()`, which is a little bit tricky.
I was inspired by how [nlepage/go-tarfs](https://github.com/nlepage/go-tarfs) stores its offsets, but we need to do it slightly differently because it's not _quite_ right.
They store [the offset - 512](https://github.com/nlepage/go-tarfs/blob/7b1a8fac13d9033f2b1fdad64486c19ad8803cb2/fs.go#L70), which is almost always correct, but for files with very long names, the name actually gets split across multiple tar headers.

The clever thing they did is store the offset to the start of the header, which allows us to implement `Info()` by using a `tar.Reader` to parse the header from the start, but we need to account for that edge case, which means we'll calculate it slightly differently.
since we know the previous file's size and starting point, we can predict the next header by rounding up to the nearest 512 byte boundary.
This allows the `tar.Reader` to handle the multiple-headers-in-a-row case for us, which also simplifies our index implementation.

## Preliminary Numbers

I wanted to get a sense for how much of a difference some of these things would make, so I tried 4 lazier variants of these ideas.

The first is baseline, just serializing the `[]tarfs.Entry`, which contains the `tar.Header`, a normalized filename string, and the offset (this is what `tarfs` implements today):

```go
type Entry struct {
	Filename string
	Header tar.Header
	Offset int64
}

type TOC struct {
	Entries []*Entry
}
```

For `ubuntu.tar` at 80MB, that came in at a whopping 1.4MB uncompressed, 69K compressed.

The second includes only the information we need (described above) and (I'm a bad scientist) flips the [array of structs](https://en.wikipedia.org/wiki/AoS_and_SoA) to a struct of arrays:

```go
type TOC struct {
	Names     []string
	Typeflags []byte
	Offsets   []int64
}
```

That dropped uncompressed size to 168K and compressed size to 30K.

The third variant encodes the offset difference from the previous entry instead of the absolute offset.

That dropped uncompressed size to 155K and compressed size to 21K.

The fourth variant divides each offset by 512 before storing it, which dropped the uncompressed size to 146K and compressed size to 20K.

```
 1357845 ubuntu.index
   69043 ubuntu.index.gz
  168765 ubuntu.index2
   30558 ubuntu.index2.gz
  155864 ubuntu.index3
   20831 ubuntu.index3.gz
  146762 ubuntu.index4
   19876 ubuntu.index4.gz
80559104 ubuntu.tar
```

If I were less lazy I'd try binary encodings instead of JSON.
Maybe later.

Okay I went back and did a 1.5 version that still does array of structs but with a `[]byte` Typeflag instead of the whole `tar.Header`.

That dropped the uncompressed size a whole lot to 29K and the compressed size to 35K.

```
287852 ubuntu.index5
 35138 ubuntu.index5.gz
```

This is a somewhat interesting but unsurprising result because array of structs (especially JSON-encoded structs) hurts the data similarity quite a bit.
Forcing all the similarly-shaped data to be closer together (and elminating object key names) is worth about 1/7 the size, apparently.

## TODO

* Implement this and compare serialized sizes.
* Compare DEFLATE with and without this weird path encoding.
* Discuss other tradeoffs.
