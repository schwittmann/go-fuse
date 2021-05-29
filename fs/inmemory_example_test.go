// Copyright 2019 the Go-FUSE Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fs_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// files contains the files we will expose as a file system
var files = map[string]string{
	"file":              "content",
	"subdir/other-file": "other-content",
}

// inMemoryFS is the root of the tree
type inMemoryFS struct {
	fs.Inode
}

// Ensure that we implement NodeOnAdder
var _ = (fs.NodeOnAdder)((*inMemoryFS)(nil))

// OnAdd is called on mounting the file system. Use it to populate
// the file system tree.
func (root *inMemoryFS) OnAdd(ctx context.Context) {
	for name, content := range files {
		dir, base := filepath.Split(name)

		p := &root.Inode

		// Add directories leading up to the file.
		for _, component := range strings.Split(dir, "/") {
			if len(component) == 0 {
				continue
			}
			ch := p.GetChild(component)
			if ch == nil {
				// Create a directory
				ch = p.NewPersistentInode(ctx, &fs.Inode{},
					fs.StableAttr{Mode: syscall.S_IFDIR})
				// Add it
				p.AddChild(component, ch, true)
			}

			p = ch
		}

		// Make a file out of the content bytes. This type
		// provides the open/read/flush methods.
		embedder := &fs.MemRegularFile{
			Data: []byte(content),
		}

		// Create the file. The Inode must be persistent,
		// because its life time is not under control of the
		// kernel.
		child := p.NewPersistentInode(ctx, embedder, fs.StableAttr{})

		// And add it
		p.AddChild(base, child, true)
	}
}

// This demonstrates how to build a file system in memory. The
// read/write logic for the file is provided by the MemRegularFile type.
func Example() {
	// This is where we'll mount the FS
	mntDir, _ := ioutil.TempDir("", "")

	root := &inMemoryFS{}
	server, err := fs.Mount(mntDir, root, &fs.Options{
		MountOptions: fuse.MountOptions{Debug: true},
	})
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Mounted on %s", mntDir)
	log.Printf("Unmount by calling 'fusermount -u %s'", mntDir)

	// Wait until unmount before exiting
	server.Wait()
}


func TestManyFilesReadDir(t *testing.T) {
	mountpoint, err := ioutil.TempDir("", "manyfiles")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(mountpoint)

	root := &inMemoryFS{}
	server, err := fs.Mount(mountpoint, root, &fs.Options{
		MountOptions: fuse.MountOptions{
			// Set to true to see how the file system works.
			Debug: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)

	fileCount := 200
	// add more files to make it interesting
	for i:=0; i< fileCount; i++ {
		ch := root.NewPersistentInode(context.Background(), &fs.Inode{},
			fs.StableAttr{Mode: syscall.S_IFDIR})
		root.AddChild(fmt.Sprintf("added_child_%d", i), ch, true)
	}
	// files are already added, increase by fileCount
	expectedChildCount := fileCount + len(files)
	defer server.Unmount()

	iterations := 1000

	for i := 0; i < iterations; i++ {

		infos, err := ioutil.ReadDir(mountpoint)
		if err != nil {
			t.Fatal("failed read dir", err)
		}

		if len(infos) != expectedChildCount {
			for _, info := range infos {
				t.Log(info.Name())
			}
			t.Fatalf("failed, got just %d entries instead of %d in iteration %d", len(infos),expectedChildCount, i)
		}
		// check uniqueness
		uniqueMap := make(map[string]int)
		for infoIdx, v := range infos {
			if k, ok := uniqueMap[v.Name()]; ok {
				t.Errorf("uniqueness fail, saw name %q idx %d at index %d before", v.Name(), k, infoIdx)
			}
			uniqueMap[v.Name()] = infoIdx
		}
		if len(uniqueMap) != expectedChildCount {
			t.Fatalf("failed, got just %d unique entries instead of %d - failed at attempt #%d ", len(uniqueMap),expectedChildCount, i)
		}
		if t.Failed() {
			return
		}
	}
}

