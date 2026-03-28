package s3client

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Object represents an S3 object or prefix.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
	StorageClass string
	ETag         string
	IsPrefix     bool
}

// Client wraps the AWS S3 client.
type Client struct {
	s3     *s3.Client
	region string
}

// New creates a new S3 Client.
func New(endpoint, accessKey, secretKey, region string) (*Client, error) {
	if region == "" {
		region = "us-east-1"
	}

	creds := credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")

	cfg := aws.Config{
		Region:      region,
		Credentials: creds,
	}

	var opts []func(*s3.Options)
	opts = append(opts, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	if endpoint != "" {
		ep := endpoint
		opts = append(opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(ep)
		})
	}

	s3Client := s3.NewFromConfig(cfg, opts...)

	return &Client{s3: s3Client, region: region}, nil
}

// ListBuckets returns a list of bucket names.
func (c *Client) ListBuckets(ctx context.Context) ([]string, error) {
	resp, err := c.s3.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(resp.Buckets))
	for _, b := range resp.Buckets {
		if b.Name != nil {
			names = append(names, *b.Name)
		}
	}
	return names, nil
}

// CreateBucket creates a new S3 bucket.
func (c *Client) CreateBucket(ctx context.Context, name, region string) error {
	input := &s3.CreateBucketInput{
		Bucket: aws.String(name),
	}
	if region != "" && region != "us-east-1" {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}
	_, err := c.s3.CreateBucket(ctx, input)
	return err
}

// DeleteBucket deletes an S3 bucket.
func (c *Client) DeleteBucket(ctx context.Context, name string) error {
	_, err := c.s3.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(name),
	})
	return err
}

// ListObjects lists objects and prefixes in a bucket at the given prefix.
func (c *Client) ListObjects(ctx context.Context, bucket, prefix string) ([]Object, error) {
	var results []Object

	paginator := s3.NewListObjectsV2Paginator(c.s3, &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, cp := range page.CommonPrefixes {
			if cp.Prefix != nil {
				results = append(results, Object{
					Key:      *cp.Prefix,
					IsPrefix: true,
				})
			}
		}

		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			// Skip the directory marker itself
			if *obj.Key == prefix {
				continue
			}
			o := Object{
				Key:      *obj.Key,
				IsPrefix: false,
			}
			if obj.Size != nil {
				o.Size = *obj.Size
			}
			if obj.LastModified != nil {
				o.LastModified = *obj.LastModified
			}
			if obj.ETag != nil {
				o.ETag = *obj.ETag
			}
			o.StorageClass = string(obj.StorageClass)
			results = append(results, o)
		}
	}

	return results, nil
}

// progressReader wraps an io.Reader and calls onUpdate with bytes read so far.
type progressReader struct {
	r        io.Reader
	total    int64
	read     int64
	onUpdate func(read, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.read += int64(n)
	if pr.onUpdate != nil {
		pr.onUpdate(pr.read, pr.total)
	}
	return n, err
}

// progressWriterAt wraps a *os.File and calls onUpdate with bytes written so far.
type progressWriterAt struct {
	w        io.WriterAt
	total    int64
	written  int64
	onUpdate func(written, total int64)
}

func (pw *progressWriterAt) WriteAt(p []byte, off int64) (int, error) {
	n, err := pw.w.WriteAt(p, off)
	pw.written += int64(n)
	if pw.onUpdate != nil {
		pw.onUpdate(pw.written, pw.total)
	}
	return n, err
}

// UploadFile uploads a local file to S3 with progress reporting.
func (c *Client) UploadFile(ctx context.Context, bucket, key, localPath string, onProgress func(int64, int64)) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}

	pr := &progressReader{
		r:        f,
		total:    fi.Size(),
		onUpdate: onProgress,
	}

	uploader := manager.NewUploader(c.s3)
	_, err = uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   pr,
	})
	return err
}

// DownloadFile downloads an S3 object to a local file with progress reporting.
func (c *Client) DownloadFile(ctx context.Context, bucket, key, localPath string, onProgress func(int64, int64)) error {
	// Get file size first
	headResp, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return err
	}

	var totalSize int64
	if headResp.ContentLength != nil {
		totalSize = *headResp.ContentLength
	}

	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	pw := &progressWriterAt{
		w:        f,
		total:    totalSize,
		onUpdate: onProgress,
	}

	downloader := manager.NewDownloader(c.s3)
	_, err = downloader.Download(ctx, pw, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

// DeleteObject deletes a single S3 object.
func (c *Client) DeleteObject(ctx context.Context, bucket, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

// DeleteObjects deletes multiple S3 objects in one batch request.
func (c *Client) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	objs := make([]types.ObjectIdentifier, 0, len(keys))
	for _, k := range keys {
		k := k
		objs = append(objs, types.ObjectIdentifier{Key: aws.String(k)})
	}

	_, err := c.s3.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &types.Delete{
			Objects: objs,
			Quiet:   aws.Bool(true),
		},
	})
	return err
}

// CopyObject copies an S3 object to a new destination.
func (c *Client) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	source := srcBucket + "/" + srcKey
	_, err := c.s3.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(dstBucket),
		Key:        aws.String(dstKey),
		CopySource: aws.String(source),
	})
	return err
}

// GetObjectMetadata returns metadata for an S3 object.
func (c *Client) GetObjectMetadata(ctx context.Context, bucket, key string) (map[string]string, error) {
	resp, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}

	meta := map[string]string{}

	if resp.ContentType != nil {
		meta["ContentType"] = *resp.ContentType
	}
	if resp.ContentLength != nil {
		meta["ContentLength"] = fmt.Sprintf("%d", *resp.ContentLength)
	}
	if resp.LastModified != nil {
		meta["LastModified"] = resp.LastModified.Format(time.RFC3339)
	}
	if resp.ETag != nil {
		meta["ETag"] = strings.Trim(*resp.ETag, `"`)
	}
	meta["StorageClass"] = string(resp.StorageClass)

	for k, v := range resp.Metadata {
		meta["x-amz-meta-"+k] = v
	}

	return meta, nil
}

// summarizeACL converts a list of grants to a canned ACL string.
func summarizeACL(grants []types.Grant) string {
	hasPublicWrite := false
	hasPublicRead := false

	for _, g := range grants {
		if g.Grantee == nil {
			continue
		}
		isAllUsers := g.Grantee.URI != nil && *g.Grantee.URI == "http://acs.amazonaws.com/groups/global/AllUsers"
		if isAllUsers {
			if g.Permission == types.PermissionWrite || g.Permission == types.PermissionFullControl {
				hasPublicWrite = true
			}
			if g.Permission == types.PermissionRead {
				hasPublicRead = true
			}
		}
	}

	if hasPublicWrite {
		return "public-read-write"
	}
	if hasPublicRead {
		return "public-read"
	}
	return "private"
}

// GetBucketACL retrieves the canned ACL for a bucket.
func (c *Client) GetBucketACL(ctx context.Context, bucket string) (string, error) {
	resp, err := c.s3.GetBucketAcl(ctx, &s3.GetBucketAclInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return "", err
	}
	return summarizeACL(resp.Grants), nil
}

// SetBucketACL sets the canned ACL for a bucket.
func (c *Client) SetBucketACL(ctx context.Context, bucket, acl string) error {
	_, err := c.s3.PutBucketAcl(ctx, &s3.PutBucketAclInput{
		Bucket: aws.String(bucket),
		ACL:    types.BucketCannedACL(acl),
	})
	return err
}

// GetObjectACL retrieves the canned ACL for an object.
func (c *Client) GetObjectACL(ctx context.Context, bucket, key string) (string, error) {
	resp, err := c.s3.GetObjectAcl(ctx, &s3.GetObjectAclInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", err
	}
	return summarizeACL(resp.Grants), nil
}

// SetObjectACL sets the canned ACL for an object.
func (c *Client) SetObjectACL(ctx context.Context, bucket, key, acl string) error {
	_, err := c.s3.PutObjectAcl(ctx, &s3.PutObjectAclInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		ACL:    types.ObjectCannedACL(acl),
	})
	return err
}
