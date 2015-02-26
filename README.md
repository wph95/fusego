[![GoDoc](https://godoc.org/github.com/jacobsa/ogletest?status.svg)](https://godoc.org/github.com/jacobsa/fuse)

This package allows for writing and mounting user-space file systems from Go. It
is a wrapper around [bazil.org/fuse][bazil], which does the heavy lifting. It
does not make use of the [bazil.org/fuse/fs][bazil-fs] sub-package, which allows
for something like an object-orientend representation of files and directories,
and contains a decent amount of canned behavior.

The chief improvements and/or differences from the bazil.org packages are:

 *  A single interface (`fuse.FileSystem`) for all of the methods that you might
    care about.

 *  No surprises in the form of magic/default behaviors. You must provide an
    implementation for every method in the interface. Embed a
    `fuseutil.NotImplementedFileSystem` struct to have default implementations
    that return `ENOSYS`.

 *  Every method, struct, and field is thoroughly documented. This may help you
    get your bearings in the world of FUSE, the Linux VFS, traditional file
    system implementations, etc., all of which tend to be very poorly
    documented.

The very large disadvantage over using the bazil.org packages is that many
features have not yet been exposed.

[bazil]: http://godoc.org/bazil.org/fuse
[bazil-fs]: http://godoc.org/bazil.org/fuse/fs
