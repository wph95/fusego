// Copyright 2015 Google Inc. All Rights Reserved.
// Author: jacobsa@google.com (Aaron Jacobs)

package fuse

import (
	"time"

	bazilfuse "bazil.org/fuse"

	"golang.org/x/net/context"
)

// An interface that must be implemented by file systems to be mounted with
// FUSE. See also the comments on request and response structs.
//
// Not all methods need to have interesting implementations. Embed a field of
// type fuseutil.NotImplementedFileSystem to inherit defaults that return
// ENOSYS to the kernel.
//
// Must be safe for concurrent access via all methods.
type FileSystem interface {
	// Look up a child by name within a parent directory. The kernel calls this
	// when resolving user paths to dentry structs, which are then cached.
	LookUpInode(
		ctx context.Context,
		req *LookUpInodeRequest) (*LookUpInodeResponse, error)

	// Forget an inode ID previously issued (e.g. by LookUpInode). The kernel
	// calls this when removing an inode from its internal caches.
	ForgetInode(
		ctx context.Context,
		req *ForgetInodeRequest) (*ForgetInodeResponse, error)

	// Open a directory inode. The kernel calls this method when setting up a
	// struct file for a particular inode with type directory, usually in
	// response to an open(2) call from a user-space process.
	OpenDir(
		ctx context.Context,
		req *OpenDirRequest) (*OpenDirResponse, error)

	// Release a previously-minted handle. The kernel calls this when there are
	// no more references to an open file: all file descriptors are closed and
	// all memory mappings are unmapped.
	//
	// The kernel guarantees that the handle ID will not be used in further calls
	// to the file system (unless it is reissued by the file system).
	ReleaseHandle(
		ctx context.Context,
		req *ReleaseHandleRequest) (*ReleaseHandleResponse, error)
}

////////////////////////////////////////////////////////////////////////
// Simple types
////////////////////////////////////////////////////////////////////////

// A 64-bit number used to uniquely identify a file or directory in the file
// system. File systems may mint inode IDs with any value except for
// RootInodeID.
//
// This corresponds to struct inode::i_no in the VFS layer.
// (Cf. http://goo.gl/tvYyQt)
type InodeID uint64

// An opaque 64-bit number used to identify a particular open handle to a file
// or directory.
//
// This corresponds to fuse_file_info::fh.
type HandleID uint64

// A distinguished inode ID that identifies the root of the file system, e.g.
// in a request to OpenDir or LookUpInode. Unlike all other inode IDs, which
// are minted by the file system, the FUSE VFS layer may send a request for
// this ID without the file system ever having referenced it in a previous
// response.
const RootInodeID InodeID = InodeID(bazilfuse.RootID)

// A generation number for an inode. Irrelevant for file systems that won't be
// exported over NFS. For those that will and that reuse inode IDs when they
// become free, the generation number must change when an ID is reused.
//
// This corresponds to struct inode::i_generation in the VFS layer.
// (Cf. http://goo.gl/tvYyQt)
//
// Some related reading:
//
//     http://fuse.sourceforge.net/doxygen/structfuse__entry__param.html
//     http://stackoverflow.com/q/11071996/1505451
//     http://goo.gl/CqvwyX
//     http://julipedia.meroh.net/2005/09/nfs-file-handles.html
//     http://goo.gl/wvo3MB
//
type GenerationNumber uint64

// Attributes for a file or directory inode. Corresponds to struct inode (cf.
// http://goo.gl/tvYyQt).
type InodeAttributes struct {
	// The size of the file in bytes.
	Size uint64
}

////////////////////////////////////////////////////////////////////////
// Requests and responses
////////////////////////////////////////////////////////////////////////

type LookUpInodeRequest struct {
	// The ID of the directory inode to which the child belongs.
	Parent InodeID

	// The name of the child of interest, relative to the parent. For example, in
	// this directory structure:
	//
	//     foo/
	//         bar/
	//             baz
	//
	// the file system may receive a request to look up the child named "bar" for
	// the parent foo/.
	Name string
}

type LookUpInodeResponse struct {
	// The ID of the child inode. The file system must ensure that the returned
	// inode ID remains valid until a later call to ForgetInode.
	Child InodeID

	// A generation number for this incarnation of the inode with the given ID.
	// See comments on type GenerationNumber for more.
	Generation GenerationNumber

	// Current ttributes for the child inode.
	Attributes InodeAttributes

	// The FUSE VFS layer in the kernel maintains a cache of file attributes,
	// used whenever up to date information about size, mode, etc. is needed.
	//
	// For example, this is the abridged call chain for fstat(2):
	//
	//  *  (http://goo.gl/tKBH1p) fstat calls vfs_fstat.
	//  *  (http://goo.gl/3HeITq) vfs_fstat eventuall calls vfs_getattr_nosec.
	//  *  (http://goo.gl/DccFQr) vfs_getattr_nosec calls i_op->getattr.
	//  *  (http://goo.gl/dpKkst) fuse_getattr calls fuse_update_attributes.
	//  *  (http://goo.gl/yNlqPw) fuse_update_attributes uses the values in the
	//     struct inode if allowed, otherwise calling out to the user-space code.
	//
	// In addition to obvious cases like fstat, this is also used in more subtle
	// cases like updating size information before seeking (http://goo.gl/2nnMFa)
	// or reading (http://goo.gl/FQSWs8).
	//
	// Most 'real' file systems do not set inode_operations::getattr, and
	// therefore vfs_getattr_nosec calls generic_fillattr which simply grabs the
	// information from the inode struct. This makes sense because these file
	// systems cannot spontaneously change; all modifications go through the
	// kernel which can update the inode struct as appropriate.
	//
	// In contrast, a FUSE file system may have spontaneous changes, so it calls
	// out to user space to fetch attributes. However this is expensive, so the
	// FUSE layer in the kernel caches the attributes if requested.
	//
	// This field controls when the attributes returned in this response and
	// stashed in the struct inode should be re-queried. Leave at the zero value
	// to disable caching.
	//
	// More reading:
	//     http://stackoverflow.com/q/21540315/1505451
	AttributesExpiration time.Time

	// The time until which the kernel may maintain an entry for this name to
	// inode mapping in its dentry cache. After this time, it will revalidate the
	// dentry.
	//
	// As in the discussion of attribute caching above, unlike real file systems,
	// FUSE file systems may spontaneously change their name -> inode mapping.
	// Therefore the FUSE VFS layer uses dentry_operations::d_revalidate
	// (http://goo.gl/dVea0h) to intercept lookups and revalidate by calling the
	// user-space LookUpInode method. However the latter may be slow, so it
	// caches the entries until the time defined by this field.
	//
	// Example code walk:
	//
	//     * (http://goo.gl/M2G3tO) lookup_dcache calls d_revalidate if enabled.
	//     * (http://goo.gl/ef0Elu) fuse_dentry_revalidate just uses the dentry's
	//     inode if fuse_dentry_time(entry) hasn't passed. Otherwise it sends a
	//     lookup request.
	//
	// Leave at the zero value to disable caching.
	EntryExpiration time.Time
}

type ForgetInodeRequest struct {
	// The inode to be forgotten. The kernel guarantees that the node ID will not
	// be used in further calls to the file system (unless it is reissued by the
	// file system).
	ID InodeID
}

type ForgetInodeResponse struct {
}

type OpenDirRequest struct {
	// The ID of the inode to be opened.
	Inode InodeID

	// Mode and options flags.
	Flags bazilfuse.OpenFlags
}

type OpenDirResponse struct {
	// An opaque ID that will be echoed in follow-up calls for this directory
	// using the same struct file in the kernel. In practice this usually means
	// follow-up calls using the file descriptor returned by open(2).
	//
	// The file system must ensure this ID remains valid until a later call to
	// ReleaseHandle.
	Handle HandleID
}

type ReleaseHandleRequest struct {
	// The handle ID to be released. The kernel guarantees that this ID will not
	// be used in further calls to the file system (unless it is reissued by the
	// file system).
	Handle HandleID
}

type ReleaseHandleResponse struct {
}
