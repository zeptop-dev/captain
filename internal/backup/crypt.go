package backup

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

// Remote copies can be sealed with age (https://age-encryption.org) before
// they leave the host: the database carries password hashes, TOTP secrets,
// payment gateway keys, node secrets and every user's subscription token,
// and a bucket is someone else's computer. age streams, so a database of
// any size is encrypted in constant memory, and the standard `age` tool
// opens the result without Captain.
//
// With a recipient (an age public key) the panel can seal but not open its
// own remote copies: the identity stays with the operator. A passphrase is
// the simpler alternative; it is stored in the panel's settings, so it
// protects the bucket, not the panel host.

// Encryption is the admin-editable part of Settings.
type Encryption struct {
	Mode       string `json:"mode"`       // "" (off), "key" or "passphrase"
	Recipient  string `json:"recipient"`  // age public key "age1…" (mode key)
	Passphrase string `json:"passphrase"` // scrypt recipient (mode passphrase)
}

// Enabled reports whether remote copies are sealed.
func (e Encryption) Enabled() bool { return e.Mode != "" }

func (e Encryption) recipient() (age.Recipient, error) {
	switch e.Mode {
	case "key":
		r, err := age.ParseX25519Recipient(strings.TrimSpace(e.Recipient))
		if err != nil {
			return nil, errors.New("encryption: the recipient must be an age public key (age1…)")
		}
		return r, nil
	case "passphrase":
		if len(e.Passphrase) < 12 {
			return nil, errors.New("encryption: the passphrase must be at least 12 characters")
		}
		return age.NewScryptRecipient(e.Passphrase)
	}
	return nil, fmt.Errorf("encryption: mode must be empty, key or passphrase")
}

// Validate checks the settings without encrypting anything.
func (e Encryption) Validate() error {
	if !e.Enabled() {
		return nil
	}
	_, err := e.recipient()
	return err
}

// sealFile encrypts src into a temporary file next to it and returns its
// path and size.
func (e Encryption) sealFile(src string) (string, int64, error) {
	rcpt, err := e.recipient()
	if err != nil {
		return "", 0, err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(src), ".upload-*.age")
	if err != nil {
		return "", 0, err
	}
	fail := func(err error) (string, int64, error) {
		out.Close()
		os.Remove(out.Name())
		return "", 0, err
	}
	w, err := age.Encrypt(out, rcpt)
	if err != nil {
		return fail(err)
	}
	if _, err := io.Copy(w, in); err != nil {
		return fail(err)
	}
	if err := w.Close(); err != nil {
		return fail(err)
	}
	fi, err := out.Stat()
	if err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(out.Name())
		return "", 0, err
	}
	return out.Name(), fi.Size(), nil
}

// GenerateKey returns a new age key pair: the recipient for the settings
// and the identity for the operator to keep. The panel stores neither the
// identity nor a copy of it.
func GenerateKey() (recipient, identity string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", err
	}
	return id.Recipient().String(), id.String(), nil
}

// Open gunzips a remote copy (decrypting it first when it is sealed) into
// w. Extract is the one that also unpacks an archive; this stays for a
// copy that is only a gzipped database.
func Open(r io.Reader, w io.Writer, identity, passphrase string) error {
	src, err := decrypt(r, identity, passphrase)
	if err != nil {
		return err
	}
	zr, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("gunzip: %w", err)
	}
	defer zr.Close()
	_, err = io.Copy(w, zr)
	return err
}

// decrypt unseals an age-encrypted copy with an identity
// ("AGE-SECRET-KEY-1…") or a passphrase; a plain copy passes through.
func decrypt(r io.Reader, identity, passphrase string) (io.Reader, error) {
	br, sealed, err := peekAge(r)
	if err != nil {
		return nil, err
	}
	if sealed {
		var ids []age.Identity
		switch {
		case identity != "":
			parsed, err := age.ParseIdentities(strings.NewReader(identity))
			if err != nil {
				return nil, fmt.Errorf("identity: %w", err)
			}
			ids = parsed
		case passphrase != "":
			id, err := age.NewScryptIdentity(passphrase)
			if err != nil {
				return nil, err
			}
			ids = []age.Identity{id}
		default:
			return nil, errors.New("this copy is encrypted: give the identity file or the passphrase")
		}
		dec, err := age.Decrypt(br, ids...)
		if err != nil {
			return nil, fmt.Errorf("decrypt: %w", err)
		}
		return dec, nil
	}
	return br, nil
}

const ageHeader = "age-encryption.org/v1\n"

func peekAge(r io.Reader) (io.Reader, bool, error) {
	head := make([]byte, len(ageHeader))
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	head = head[:n]
	return io.MultiReader(bytes.NewReader(head), r), string(head) == ageHeader, nil
}
