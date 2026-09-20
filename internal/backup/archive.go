package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// An encrypted off-site copy carries the database *and* config.yaml, as a
// tar.gz sealed with age: the database has the accounts, nodes and their
// token hashes, but base_url and the payment gateway keys live only in the
// config file, and a panel restored without it comes up unable to take
// money. The config is only ever included when encryption is on — it holds
// those keys in plain text, and a bucket is someone else's computer.
//
// Without encryption the remote copy stays what it was: gzipped database.

// Archive member names. Anything else in the archive is ignored on open.
const (
	memberDB     = "captain.db"
	memberConfig = "config.yaml"
)

// archiveFiles writes db and (when non-empty) cfg into a temporary tar.gz
// next to db, and returns its path and size.
func archiveFiles(db, cfg string) (string, int64, error) {
	out, err := os.CreateTemp(filepath.Dir(db), ".upload-*.tar.gz")
	if err != nil {
		return "", 0, err
	}
	fail := func(err error) (string, int64, error) {
		out.Close()
		os.Remove(out.Name())
		return "", 0, err
	}
	zw := gzip.NewWriter(out)
	tw := tar.NewWriter(zw)
	add := func(name, src string, mode int64) error {
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: st.Size(), ModTime: st.ModTime(), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return err
	}
	if err := add(memberDB, db, 0o600); err != nil {
		return fail(err)
	}
	if cfg != "" {
		if err := add(memberConfig, cfg, 0o600); err != nil {
			return fail(fmt.Errorf("%s: %w", memberConfig, err))
		}
	}
	if err := tw.Close(); err != nil {
		return fail(err)
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	st, err := out.Stat()
	if err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(out.Name())
		return "", 0, err
	}
	return out.Name(), st.Size(), nil
}

// Extract turns a remote copy back into files on disk: it decrypts when the
// copy is sealed, unpacks it, and writes the database to dbPath and any
// config next to it. A copy that is only a gzipped database (encryption
// off, or made before this release) writes just the database. Existing
// files are never overwritten. Returns the paths written.
func Extract(r io.Reader, dbPath, identity, passphrase string) ([]string, error) {
	plain, err := decrypt(r, identity, passphrase)
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(plain)
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	defer zr.Close()

	head, isTar, err := peekTar(zr)
	if err != nil {
		return nil, err
	}
	if !isTar {
		if err := writeNew(dbPath, head, 0o600); err != nil {
			return nil, err
		}
		return []string{dbPath}, nil
	}
	var written []string
	tr := tar.NewReader(head)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return written, err
		}
		// Only the two names this archive is made of: a tar entry must
		// never decide where we write.
		var dest string
		switch filepath.Base(h.Name) {
		case memberDB:
			dest = dbPath
		case memberConfig:
			dest = filepath.Join(filepath.Dir(dbPath), memberConfig)
		default:
			continue
		}
		if err := writeNew(dest, tr, 0o600); err != nil {
			return written, err
		}
		written = append(written, dest)
	}
	if len(written) == 0 {
		return nil, errors.New("backup: the archive holds no database")
	}
	return written, nil
}

func writeNew(path string, src io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, src); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}

// peekTar reports whether the stream is a tar archive (the "ustar" magic
// sits at offset 257 of the first block) and returns a reader over the
// whole stream again.
func peekTar(r io.Reader) (io.Reader, bool, error) {
	const magicAt = 257
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	head = head[:n]
	isTar := n >= magicAt+5 && string(head[magicAt:magicAt+5]) == "ustar"
	return io.MultiReader(bytes.NewReader(head), r), isTar, nil
}

// remoteName is the object name for a snapshot: the archive shape when the
// config travels with it, the old gzipped database otherwise.
func remoteName(snapshot string, archived, sealed bool) string {
	name := filepath.Base(snapshot)
	if archived {
		name = name[:len(name)-len(filepath.Ext(name))] + ".tar.gz"
	} else {
		name += ".gz"
	}
	if sealed {
		name += ".age"
	}
	return name
}
