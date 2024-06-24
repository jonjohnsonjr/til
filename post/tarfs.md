# tarfs

I wrote a little [`tarfs`](https://github.com/jonjohnsonjr/targz/tree/main/tarfs) package that I've found very useful.
If you ever have to work with tar files in Go, you might find it useful too.
Even if you don't, you might still learn something interesting by reading this post.

## What's a tar file?

Tar is short for "tape archive".
It's one of many ways to take a bunch of files and stuff them into a single file.
This is useful because many tools are built to handle single files and not entire filesystems.
You can also ignore the differences in filesystem implementations by forcing everything to serialize to a tar file.

Notably, since the tar format is meant for tape archives, it's not designed to allow for quick random access.
Tape drives are relatively slow, and archives imply that they aren't meant for online access.

There are several flavors of tar, but let's ignore that for now and focus on a simplified model of the format.

A tar file is a just a series of records that alternate between a 512-byte header and the actual file contents (if there are any).
The header contains metadata for the next file, including its name and size, which tells you how many bytes to read after the header.
After the end of each file, there is zero padding until the next 512-byte boundary.

After the last file, there are (at least) two 512 byte blocks of zeros that indicate the End Of Archive (EOA).

<!--
```dot
digraph G {
    tar [shape="record", label="{header|content|header|content|...|EOA}"];
}
```
-->

[![tar](./tarfs/tar.svg)](./tarfs/tar.svg)

This format is easy to both produce and consume in a streaming manner, which means it's amenable to bash one-liners, which probably explains its ubiquity. 

Given the amount of zero-padding and text duplication common in the headers, tars are generally compressed with gzip to form a `.tar.gz` file, which is similarly amenable to bash one-liners.
 
## How do you work with tar files in Go?

If you search for how to do this, you'll find a [straightforward implementation](https://medium.com/@skdomino/taring-untaring-files-in-go-6b07cf56bc07).

<details><summary>(inlined implementation)</summary>

```
// Untar takes a destination path and a reader; a tar reader loops over the tarfile
// creating the file structure at 'dst' along the way, and writing any files
func Untar(dst string, r io.Reader) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()

		switch {

		// if no more files are found return
		case err == io.EOF:
			return nil

		// return any other error
		case err != nil:
			return err

		// if the header is nil, just skip it (not sure how this happens)
		case header == nil:
			continue
		}

		// the target location where the dir/file should be created
		target := filepath.Join(dst, header.Name)

		// the following switch could also be done using fi.Mode(), not sure if there
		// a benefit of using one vs. the other.
		// fi := header.FileInfo()

		// check the file type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}

		// if it's a file create it
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			// copy over contents
			if _, err := io.Copy(f, tr); err != nil {
				return err
			}
			
			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			f.Close()
		}
	}
}
```

</details>

This kind of works, but there are a handful of problems.

### 1. Safety

Unless you can completely trust the tar file (i.e. you created it yourself), it's not safe to just extract it to a temporary directory.
This is due to a category of vulnerability called [Zip Slip](https://security.snyk.io/research/zip-slip-vulnerability).
A maliciously crafted tar file can have paths that escape the target directory using relative filenames (like `../../evil.sh`) and overwrite existing files with arbitrary content.

### 2. Completeness

This implementation only handles directories and regular files.
There are [other file types](https://pkg.go.dev/archive/tar#pkg-constants) that will just get silently ignored, most notably symlinks and hardlinks.

It also assumes that the target directory already exists for each regular file.
Most tar implementations do indeed include any directories before files in those directories, but this isn't guaranteed, so you may end up trying to write a file to a non-existent directory.

### 3. Orthogonality

This is just me being pedantic but this function assumes the input is gzipped, which isn't necessarily the case.
Sometimes you are just dealing with a tar file and don't need to gunzip it first.
In fact, assuming you need to gunzip it first constrains your thinking to serial access patterns, which makes it difficult to see a nicer solution.

## Access Patterns

One problem I'm fairly familiar with is reading a docker archive tarball.

I've described the layout of a docker archive [here](https://github.com/google/go-containerregistry/tree/main/pkg/v1/tarball#structure), but I'll repeat the relevant parts here.
A docker archive is a tarball that contains a `manifest.json` file and a bunch of other blob files.
The `manifest.json` file is an array, so it can contain multiple images.
In order to know which blobs you need to extract for a given image (and in which order), you need to open and parse the `manifest.json` file first.
This means we really want random access to the tar filesystem, so how can we get it?

There are a couple strategies here.

### 1. Just untar it

The most obvious thing to do is untar the entire thing and access it through the normal filesystem.

[Docker](https://github.com/moby/moby/blob/d1273b2b4a1fa511890035fbf75d299f345c5aaa/image/tarexport/load.go#L33) does this, but it's a lot more complicated than you might expect.
It involves chroots, unshare, re-execing itself, unix pipes, and a bunch of additional complexity to convince the go runtime to play nicely with all of that.

There's also file metadata within the tar that you may or may not want to preserve like permissions, ownership, access times, and xattrs (which dictate capabilities), all of which have security implications.

From [comments on their implementation](https://github.com/moby/moby/blob/d1273b2b4a1fa511890035fbf75d299f345c5aaa/pkg/chrootarchive/archive_unix.go#L52-L53):

```
// The implementation is buggy, and some bugs may be load-bearing.
// Here be dragons.
```

Note that I'm not trying to disparage docker's implementation at all, it just reflects the complicated reality of the problem.
Sometimes you do actually need to write out the files to disk, which indeed requires a lot of this ceremony if you have to handle untrusted inputs.

### 2. Schlemiel the Painter's Algorithm

Let's look at a different approach to the same problem.

This is in go-containerregistry, which means I'm at least partially responsible for it, so it's fair game to disparage.

Rather than untarring the entire thing to disk to give us random access, we can instead just re-open the tar file and read through it again until we find what we're looking for.
[This ends up being pretty slow](https://en.wikichip.org/wiki/schlemiel_the_painter%27s_algorithm), but it also avoid all the complexity and safety concerns of untarring it to disk.

## A better way

Both of these solutions were written before go introduced the `io/fs` package, which provides a really nice filesystem abstraction.
In fact, `fs.FS` is exactly the interface you usually want for a tar file.
It's a bit of a shame that the `archive/tar` package doesn't implement this interface, but it's not too hard to do it yourself, which is exactly what I've done in my `tarfs` package.

Notably, I'm not the first person to do this, but I do like my implementation the best.

### How `tarfs` works

When creating a new `tarfs.FS`, we first have to iterate over the whole tar file to build an index of the file offsets.
This is a little slow, but it's actually a requirement for correctness, and it can be amortized over many reads by saving the index for later use.

The index would look something like this:

<!--
```dot
digraph G {
    idx [shape="record", label="{{header|offset}|{header|offset}|...}"];
}
```
-->

[![index](./tarfs/index.svg)](./tarfs/index.svg)


Notice that this would only be 520 bytes per file instead of the entire file size, so it's relatively small.
Also, given that headers are pretty similar, this index should compress very well.
There is probably a much more compact format we could store these things in, but it's nice to have the identical data.

In theory, we could store only the offsets to the header data in the original tar file so that we don't store it twice, but by duplicating the header data, we can render the entire filesystem from just the index without actually having access to the original tar file.
This technique is useful to me for quickly rendering results on dag.dev, since I can just store the indexes locally and render the filesystem view without hitting the registry.

Separately, we have an in-memory mapping of filename to the index entry to give us random access to any file metadata.
This is just a `map[string]int` that gives us the array index of the file entry in the index described above.

<!--
digraph G {
    names [shape="record", label="{{name|index}|{name|index}|{name|index}|...}"];
}
-->

[![names](./tarfs/names.svg)](./tarfs/names.svg)

With this structure, every `tarfs.FS.Open()` is just two map accesses, and every `Read()` is just a single `ReadAt()` call on the underlying `os.File`.

Also, because I know that ~all my workloads end up immediately calling `fs.WalkDir` on the result, as part of the indexing I generate a pre-sorted `map[string][]fs.DirEntry` so that calling `ReadDir` doesn't have to allocate.

<!--
digraph G {
    names [shape="record", label="{{dir|{file|file}}|{dir|{file}}|{dir|{file|file|file}}|...}"];
}
-->

[![dirs](./tarfs/dirs.svg)](./tarfs/dirs.svg)

For the stateless `FS.ReadDir` method, we just return the whole `[]fs.DirEntry` as-is, and for the stateful `File.ReadDir` method, we just return a slice of the requested size and maintain a cursor for the given position.
Somewhat unexpectedly, implementing this part was the most difficult part for me when I was trying to get `tarfs` to pass `fstest.TestFS`.

The star of the show here is really [`io.ReaderAt`](./readat.md), which enables concurrent and efficient access to the bytes of the underlying tar file.
This will be even more apparent if I ever write about how this [composes](https://github.com/jonjohnsonjr/targz/blob/main/README.md#targz) with `gsip` and Range requests.

### Performance

To compare these approaches, I've summoned a random tar file from Docker Hub:

```
crane blob ubuntu@sha256:9c704ecd0c694c4cbdd85e589ac8d1fc3fd8f890b7f3731769a5b169eb495809 | gunzip > ubuntu.tar
```

Let's get a general sense of its properties:

```
$ wc -c ubuntu.tar
 80559104 ubuntu.tar # 80.6MB

$ tarp < ubuntu.tar | jq '.Typeflag' | sort | uniq -c
2579 48 # Regular files
   2 49 # Hardlinks
 197 50 # Symlinks
 658 53 # Directories

$ tarp < ubuntu.tar | jq 'select(.Typeflag == 48) | .Size' | jq -s 'add'
78046512 # 78.0MB of regular file data
```

I've written three methods for accessing tar files:

1. `untar` which writes everything to a temporary directory, then deletes it afterwards,
2. `tarfs` which uses the `tarfs` package, and
3. `scantar` which re-scans through to access each file.

The dumb benchmark I have here is to read three files and write them to stdout.

```
untar < ubuntu.tar  0.04s user 0.54s system 98% cpu 0.588 total
tarfs < ubuntu.tar  0.02s user 0.01s system 97% cpu 0.028 total
scantar < ubuntu.tar  0.02s user 0.01s system 93% cpu 0.033 total
```

You can see that `untar` is the slowest because it has to actually write everything out to disk and also clean it up.

Since `tarfs` and `scantar` are read-only operations, they are both super fast.
We can cheat a little bit to give tarfs an advantage by looking at a bunch of files that are near the end of the tar file.

```
tarfs < ubuntu.tar  0.02s user 0.01s system 94% cpu 0.035 total
scantar < ubuntu.tar  0.08s user 0.05s system 98% cpu 0.128 total
```

We can see that `tarfs` is basically unaffected, whereas `scantar` is much slower because it's reading through almost the whole tar file for every file access.

A more realistic use case might be walking to `Walk` every file in the tar, so I did that to compare `tarfs` and `untar`, generating this graph:

[![tarfs vs untar](./tarfs/latency.png)](./tarfs/latency.png)

The Y axis is in microseconds, the X axis is mostly each file being accessed, but I also added a data point for the RemoveAll for `untar` at the end.

Why does it look like this?

With `tarfs`, we have a single `open` syscall, then the initial scan through to generate the index takes a bunch of `read` syscalls, then "walking" the filesystem is entirely in userspace.
Reading each individual file is also exlusively `pread` syscalls.
You can see the overall time spent is almost evenly split between the initial scan and the subsequent accesses.
Indexing the tar takes ~26 milliseconds, then reading all the files takes another ~17ms.
This makes sense, because indexing has to read through every byte and parse the tar headers, whereas we are reading only the file data portions when we walk the filesystem.

With `untar`, not only do we have to `open`, `write`, and `close` (and sometimes `mkdir`) each file during the initial untar.
We also have to `open`, `read`, and `close` each file during the subsequent accesses, so we have ~3x the overhead just for reading each file.
We have to call `getdirentries` to then walk the filesystem, and finally the tmpdir cleanup requires `unlinkat` syscalls for each file.
Untarring everything takes ~360ms, then reading all the files takes ~100ms, then cleanup takes ~230ms.
Less than 15% of the execution time is spent actually accessing the data.

Note that I'm not showing the `scantar` results here because we don't have a way to Walk the filesystem without some preprocessing.
In cases where your workload is actually this contrived, the existing `tar.Reader` API is probably fine for you, and you would expect this to take about as long as `tarfs` takes to index the tar file (~26ms).


## When not to use tarfs

### Sometimes `tar.Reader` is fine

There are two scenarios where you might just want to use the plain tar package and not `tarfs`.

1. You only have an `io.Reader` and not an `io.ReaderAt`, e.g. if you're streaming the tar file from a network connection.
2. You are looking for a single entry in the tar file and are okay with exiting early.

If you find yourself in the second secnario, you should be careful.
It is not generally correct to assume that the first matching entry in a tar file is what you want to extract because tar files may contain duplicate entries.

Let me explain.

#### Last write wins

First, let's create a tar file with a single entry:

```
echo -n "Hello, world" > hello.txt && tar -cf out.tar hello.txt
```

We can list the files:

```
tar -tvf out.tar
-rw-r--r--  0 jonjohnson staff      12 Jun 20 16:21 hello.txt
```

We can even extract the file:
```
tar -Oxf out.tar hello.txt
Hello, world
```

Instead of using `-c` to create a tar file, we can use `-r` to update it:

```
echo '!' >> hello.txt && tar -rf out.tar hello.txt
```

And we'll see that there are two entries:

````
tar -tvf out.tar
-rw-r--r--  0 jonjohnson staff      12 Jun 20 16:21 hello.txt
-rw-r--r--  0 jonjohnson staff      14 Jun 20 16:21 hello.txt
````

If we try to extract the file as before, we'll get both:
```
tar -Oxf out.tar hello.txt
Hello, worldHello, world!
```

But if you actually write it out to disk, you'll see that the last file wins:

```
mkdir out && tar -xf out.tar -C out && cat out/hello.txt
Hello, world!
```

For this reason, in the general case, you should read through the entire tar file before returning anything, because the file you care about might have been overwritten.

There are definitely situations where you know the layout of a tar file ahead of time (e.g. an APK package) where you know you can stop reading when you get to the first matching file, but a general purpose tar library can't make that assumption.

### You want to Open() Symlinks and Hardlinks

Right now `io/fs` is [kind of weird about symlinks](https://github.com/golang/go/issues/49580), and I'm waiting for all of that to shake out before I commit to anything in particular.

In most of my uses of `tarfs`, I actually don't want to `Open()` symlinks or hardlinks because I want to preserve them in the ouput archives I'm producing.
There's a `Sys` method on `fs.FileInfo` that I use to return the original `*tar.Header`, which is what I use in practice for handling these things.
This is one place where the performance stuff above is a little misleading, but there are only two hardlinks in the entire archive, so it's not significant enough to affect the results.

(Since the time of writing, I've added symlink chasing to `tarfs`, but I don't care to update the rest of this post.)

## Disclaimer

I wouldn't attempt to put `tarfs` into production quite yet, but eventually I will extract it from its experimental home under [targz](https://github.com/jonjohnsonjr/targz).
