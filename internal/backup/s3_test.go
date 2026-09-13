package backup

import (
	"net/http"
	"testing"
	"time"
)

// AWS's documented GET Object example (SigV4 S3 developer guide):
// examplebucket, /test.txt, Range bytes=0-9, 2013-05-24T00:00:00Z.
func TestSigV4Vector(t *testing.T) {
	s := &S3{Region: "us-east-1", AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}
	req, _ := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	s.sign(req, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestObjectURL(t *testing.T) {
	s := &S3{Endpoint: "https://abc.r2.cloudflarestorage.com", Bucket: "bk", Prefix: "p/q"}
	u, _ := s.objectURL(s.key("captain-1.db.gz"))
	if u.String() != "https://bk.abc.r2.cloudflarestorage.com/p/q/captain-1.db.gz" {
		t.Fatal(u.String())
	}
	s.PathStyle = true
	u, _ = s.objectURL(s.key("a b.gz"))
	if u.String() != "https://abc.r2.cloudflarestorage.com/bk/p/q/a%20b.gz" {
		t.Fatal(u.String())
	}
}
