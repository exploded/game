// Package backup uploads a gzipped snapshot of the database to Amazon S3.
package backup

import (
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	_ "modernc.org/sqlite"
)

// putter is the part of *s3.Client the uploader uses, so tests can fake it.
type putter interface {
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// Uploader puts snapshots of one database into one bucket.
type Uploader struct {
	client putter
	bucket string
	dbPath string
}

// New returns an Uploader for the database at dbPath, or nil when the bucket
// or keys are empty: backups are off.
func New(region, bucket, accessKey, secretKey, dbPath string) *Uploader {
	if bucket == "" || accessKey == "" || secretKey == "" {
		return nil
	}
	return &Uploader{
		client: s3.NewFromConfig(aws.Config{
			Region:      region,
			Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		}),
		bucket: bucket,
		dbPath: dbPath,
	}
}

// Bucket is the bucket the snapshots go to.
func (u *Uploader) Bucket() string { return u.bucket }

// Key is the object name for day's snapshot (YYYY-MM-DD). A second upload on
// the same day replaces the first.
func Key(day string) string { return "game-" + day + ".db.gz" }

// Upload snapshots the database, gzips it and puts it in the bucket as
// Key(day). It returns the gzipped size.
//
// The snapshot is taken with VACUUM INTO on a connection of its own: the
// app's pool has one connection, and a few seconds' snapshot on it would hold
// up every request. In WAL mode the snapshot only reads, so the app keeps
// writing while it runs. The working files go next to the database, not in
// /tmp, which is a RAM disk on the server.
func (u *Uploader) Upload(ctx context.Context, day string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	abs, err := filepath.Abs(u.dbPath)
	if err != nil {
		return 0, err
	}
	snap := filepath.Join(filepath.Dir(abs), "backup-snapshot.db")
	gz := snap + ".gz"
	defer os.Remove(snap)
	defer os.Remove(gz)

	if err := snapshot(ctx, abs, snap); err != nil {
		return 0, err
	}
	size, err := compress(snap, gz)
	if err != nil {
		return 0, err
	}
	// The snapshot isn't needed once it's compressed; free the disk now.
	os.Remove(snap)

	f, err := os.Open(gz)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	key := Key(day)
	_, err = u.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(u.bucket),
		Key:           aws.String(key),
		Body:          f,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String("application/gzip"),
	})
	if err != nil {
		return 0, fmt.Errorf("s3 put %s/%s: %w", u.bucket, key, err)
	}
	return size, nil
}

// snapshot writes a consistent copy of the database at src to dst.
func snapshot(ctx context.Context, src, dst string) error {
	// VACUUM INTO won't overwrite, so clear what a crashed run left.
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove old snapshot: %w", err)
	}
	conn, err := sql.Open("sqlite", src+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	return nil
}

// compress gzips src into dst and returns dst's size.
func compress(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	zw := gzip.NewWriter(out)
	if _, err := io.Copy(zw, in); err != nil {
		return 0, fmt.Errorf("gzip: %w", err)
	}
	if err := zw.Close(); err != nil {
		return 0, fmt.Errorf("gzip: %w", err)
	}
	if err := out.Close(); err != nil {
		return 0, err
	}
	st, err := os.Stat(dst)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}
