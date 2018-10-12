// Package prune contains functions that allow you to manipulate data on the storage.
//
// OVERVIEW
//
// Since the registry server stores metadata in the etcd database, there are situations
// of the desynchronization of the database and the registry storage.
//
// 1. If the data is removed from the etcd database, but not removed from the storage,
// this leads to a waste of space in the storage. At the moment, the hard pruning
// can help with this problem if you do not need the blobs anymore.
//
// 2. If the data is removed from the registry storage, but not removed from etcd database,
// this leads us to broken cluster. The images that are managed by registry can not be used.
// In some of these situations the best thing we can do is provide a tool which reports on
// what images are broken so the user can attempt to recover their system. In other cases,
// we can restore the data in the etcd database based on data from the storage.
//
// HARD PRUNE
//
// This mode allows you to delete blobs that are no longer referenced in the etcd
// database (garbage) from storage and reduce the used space on the storage.
//
// RECOVERY
//
// This mode is opposite to the HARD PRUNE. In this mode, we try to restore metadata
// from the data on the storage. There are two modes of this: check and check+recover.
//
// This mode covers the following cases:
//
// 1. Show broken imagestream tags that refer to non-existent images on the storage.
// In this case, you can either delete such a tag, or re-push the image pointed to by this tag.
//
// 2. Show broken images that refer to non-existent blobs on the storage.  In this case,
// you can re-push any imagestream tag that point to this image. Since the images are global,
// it's enough to re-push only one image to fix all the imagestream tags that refer to it.
//
// 3. Show and restore either entire imagestreams or individual imagestream tags inside it
// that exists on the storage, but removed from etcd database. Tag names will look
// like: lost-found-<IMAGE-DIGEST> because the name of the tag is not stored on the storage
// and can't be restored. You can make a new tag with a different name.
//
// Note: labels and anotations that were assigned on the image or imagestream will not be
// restored because they were stored only in the etcd database, but not on the storage.
//
package prune
