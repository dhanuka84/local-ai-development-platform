package sourceadapter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
	"github.com/dhanuka84/hybrid-ai-platform/internal/mcpclient"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Credential struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

func readS3(ctx context.Context, v View, q domain.SourceQuery) (out domain.SourceEnvelope, err error) {
	raw, err := mcpclient.ReadToken(v.Backend.CredentialFile)
	if err != nil {
		return out, err
	}
	var credential S3Credential
	if execution.DecodeProposal([]byte(raw), &credential) != nil || credential.AccessKey == "" || credential.SecretKey == "" {
		return out, errors.New("invalid S3 credential file")
	}
	u, err := url.Parse(v.Backend.Endpoint)
	if err != nil {
		return out, err
	}
	var transport http.RoundTripper = http.DefaultTransport
	if u.Scheme == "http" {
		transport = &http.Transport{DialContext: localPlainDial, DisableKeepAlives: true}
	}
	client, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(credential.AccessKey, credential.SecretKey, ""), Secure: u.Scheme == "https", Transport: transport})
	if err != nil {
		return out, err
	}
	info, err := client.StatObject(ctx, v.Backend.Bucket, v.Backend.Object, minio.StatObjectOptions{VersionID: v.Backend.VersionID})
	if err != nil {
		return out, err
	}
	if info.ETag == "" || v.Backend.ExpectedETag != "" && info.ETag != v.Backend.ExpectedETag {
		return out, domain.ErrVersionConflict
	}
	options := minio.GetObjectOptions{VersionID: info.VersionID}
	if err = options.SetMatchETag(info.ETag); err != nil {
		return out, err
	}
	object, err := client.GetObject(ctx, v.Backend.Bucket, v.Backend.Object, options)
	if err != nil {
		return out, err
	}
	defer object.Close()
	readInfo, err := object.Stat()
	if err != nil {
		return out, err
	}
	if readInfo.ETag != info.ETag {
		return out, domain.ErrVersionConflict
	}
	if info.Size > v.Contract.MaxBytes || info.ETag == "" {
		return out, domain.ErrBudgetExhausted
	}
	body, err := io.ReadAll(io.LimitReader(object, v.Contract.MaxBytes+1))
	if err != nil || int64(len(body)) > v.Contract.MaxBytes {
		return out, domain.ErrBudgetExhausted
	}
	var stored domain.SourceEnvelope
	if execution.DecodeProposal(body, &stored) != nil || stored.SchemaVersion != v.Contract.SchemaVersion || stored.Start.After(q.Start) || stored.End.Before(q.End) || stored.Watermark.IsZero() || len(stored.Rows) > v.Backend.MaxScanRecords {
		return out, errors.New("lake snapshot does not cover the requested contract")
	}
	out = envelope(v, q)
	out.Revision = "s3:" + domain.Digest(body)
	out.Snapshot = "etag:" + info.ETag
	if info.VersionID != "" {
		out.Snapshot += ";version:" + info.VersionID
	}
	out.Watermark = stored.Watermark
	out.Complete = stored.Complete
	for _, row := range stored.Rows {
		if err = appendRow(&out, q, row); err != nil {
			return out, err
		}
	}
	return out, nil
}
