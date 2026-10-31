package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type fakeS3 struct {
	key, bucket string
	length      int64
	body        []byte
}

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.bucket, f.key, f.length = aws.ToString(in.Bucket), aws.ToString(in.Key), aws.ToInt64(in.ContentLength)
	b, err := io.ReadAll(in.Body)
	f.body = b
	return &s3.PutObjectOutput{}, err
}

// The uploaded object is a gzipped database with the live data in it, taken
// while the app's own connection is open in WAL mode, and no working files
// are left behind.
func TestUploadSnapshotsLiveDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "game.db")
	live, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	live.SetMaxOpenConns(1)
	if _, err := live.Exec(`CREATE TABLE users (name TEXT); INSERT INTO users VALUES ('rob')`); err != nil {
		t.Fatal(err)
	}

	fake := &fakeS3{}
	u := &Uploader{client: fake, bucket: "b", dbPath: dbPath}
	size, err := u.Upload(context.Background(), "2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	if fake.bucket != "b" || fake.key != "game-2026-10-31.db.gz" {
		t.Errorf("put %s/%s", fake.bucket, fake.key)
	}
	if size != int64(len(fake.body)) || fake.length != size {
		t.Errorf("size %d, content length %d, body %d bytes", size, fake.length, len(fake.body))
	}

	zr, err := gzip.NewReader(bytes.NewReader(fake.body))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "restored.db")
	if err := os.WriteFile(restored, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := sql.Open("sqlite", restored)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var name string
	if err := r.QueryRow(`SELECT name FROM users`).Scan(&name); err != nil || name != "rob" {
		t.Errorf("restored name %q, err %v", name, err)
	}

	for _, leftover := range []string{"backup-snapshot.db", "backup-snapshot.db.gz"} {
		if _, err := os.Stat(filepath.Join(dir, leftover)); !os.IsNotExist(err) {
			t.Errorf("%s left behind", leftover)
		}
	}
}

func TestNewIsOffWithoutSettings(t *testing.T) {
	if New("ap-southeast-2", "", "key", "secret", "game.db") != nil {
		t.Error("no bucket should mean no uploader")
	}
	if New("ap-southeast-2", "bucket", "", "", "game.db") != nil {
		t.Error("no keys should mean no uploader")
	}
}
