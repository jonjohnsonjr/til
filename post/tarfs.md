# tarfs

I wrote a little [`tarfs`](https://github.com/jonjohnsonjr/targz/tree/main/tarfs) package that I've found very useful.
If you ever have to work with tar files in Go, you might find it useful too.
Even if you don't, you might still learn something interesting by reading this post.

## What's a tar file?

Tar is short for "tape archive".
I'ts one of many ways to take a bunch of files and stuff them into a single file.
This is useful because many tools are built to handle single files and not entire filesystems.
You can also ignore the differences in filesystem implementations by forcing everything to serialize to a tar file.

Notably, since the tar format is meant for tape archives, it's not designed to allow for quick random access.
Tape drives are relatively slow, and archives imply that they aren't meant for online access.

TODO: Actual layout.

## Access Patterns

If you search for how to do this, you'll find a [straightforward implementation](https://medium.com/@skdomino/taring-untaring-files-in-go-6b07cf56bc07).
There are a couple of problems with this.

### Read a single file

### Read multiple files with random access

One problem I'm fairly familiar with is reading a docker archive tarball.

I've described the layout of a docker archive [here](https://github.com/google/go-containerregistry/tree/main/pkg/v1/tarball#structure), but I'll repeat the relevant parts here.
A docker archive is a tarball that contains a `manifest.json` file and a bunch of other blob files.
The `manifest.json` file is an array, so it can contain multiple images.
In order to know which blobs you need to extract for a given image (and in which order), you need to open and parse the `manifest.json` file first.
This means we really want random access to the tar filesystem, so how can we get it?

There are a couple strategies here.

#### 1. Just untar it

The most obvious thing to do is untar the entire thing and access it through the normal filesystem.

[Docker](https://github.com/moby/moby/blob/d1273b2b4a1fa511890035fbf75d299f345c5aaa/image/tarexport/load.go#L33) does this, but it's a lot more complicated than you might expect.
It's not safe to just untar the entire thing to a temporary directory because of [Zip Slip](https://security.snyk.io/research/zip-slip-vulnerability).
A maliciously crafted tar file can have paths that escape the target directory using relative filenames (like `../../evil.sh`) and overwrite existing files with arbitrary content.

There's also file metadata within the tar that you may or may not want to preserve (like permissions, ownership, access times) which has security implications.

From [comments on their implementation](https://github.com/moby/moby/blob/d1273b2b4a1fa511890035fbf75d299f345c5aaa/pkg/chrootarchive/archive_unix.go#L52-L53):

```
// The implementation is buggy, and some bugs may be load-bearing.
// Here be dragons.
```

Note that I'm not trying to disparage docker's implementation at all, it just reflects the complicated reality of the problem.

#### 2. Buffer everything

#### 3. Schlemiel the Painter's Algorithm

Let's look at a different approach to the same problem.

In go-containerregistry, we 


## Last write wins

First I run `tar -cf out.tar tar.md` to add this file to the archive.

Extracting this file from `out.tar` you can see it just has the first line:

```
$ tar -Oxf out.tar tar.md
# tar
```

If instead of `-c` I use `tar -rf out.tar tar.md` to append `tar.md` to `tar.out`, you can see two entries:

```
$ tar -tvf out.tar
TODO
```

And if I try to extract `tar.md`, you'll see I get both entries.

````
$ tar -Oxf out.tar tar.md
TODO
````

But if you actually write it out to disk, you'll see that the last file wins:

```
mkdir out && tar -xf out.tar -C out && ls -al out
TODO
```
