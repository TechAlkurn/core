package uploader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TechAlkurn/core/config"
	"github.com/TechAlkurn/core/cons"
	"github.com/TechAlkurn/core/db/activerecord"

	"cloud.google.com/go/storage"
	mediaimage "github.com/TechAlkurn/core/components/image"
	"github.com/TechAlkurn/core/lib"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nfnt/resize"
)

// storagePath returns the public base URL for stored files. It is read at call
// time (not package init) because config.InitEnv loads .env after init runs.
func storagePath() string {
	if config.IsAwsS3 {
		return os.Getenv("S3_CDN_STORAGEPATH")
	}
	config.Log.Printf("STORAGE_PATH: %v\n", os.Getenv("STORAGE_PATH"))

	return os.Getenv("STORAGE_PATH")
}

type CoreMedia struct {
	Id        uint32  `gorm:"primaryKey;autoIncrement;column:id;type:int(10) unsigned;not null;" json:"id"`
	Guid      *string `gorm:"uniqueIndex:udx_guid;column:guid;type:varchar(255);default:null;" json:"guid"`
	Alt       *string `gorm:"column:alt;type:varchar(100);default:null;" json:"alt"`
	Title     *string `gorm:"column:title;type:varchar(100);default:null;" json:"title"`
	Filename  *string `gorm:"column:filename;type:varchar(255);default:null;" json:"filename"`
	Filesize  *int32  `gorm:"column:filesize;type:int(11);default:null;" json:"filesize"`
	Path      *string `gorm:"column:path;type:text;default:null;" json:"path"`
	Mimetype  *string `gorm:"column:mimetype;type:varchar(255);default:null;" json:"mimetype"`
	Status    *int8   `gorm:"column:status;type:tinyint(4);not null;default:1" json:"status"`
	CreatedAt uint32  `gorm:"column:created_at;type:int(11) unsigned;not null;autoCreateTime;" json:"created_at"`
	UpdatedAt uint32  `gorm:"column:updated_at;type:int(11) unsigned;not null;autoCreateTime;autoUpdateTime;" json:"updated_at"`
}

// TableName must be declared here: without it the promoted
// ActiveRecord.TableName is used, which resolved to "coremedia".
func (m *CoreMedia) TableName() string {
	return "core_media"
}

func (m *CoreMedia) GetPath() string {
	if m == nil || m.Path == nil {
		return ""
	}
	return *m.Path
}

func (m *CoreMedia) GetMimetype() string {
	if m == nil || m.Mimetype == nil {
		return ""
	}
	return *m.Mimetype
}

type TransformCoreMedia struct {
	*CoreMedia
	// Mimetype shadows CoreMedia.Mimetype with the top-level type ("image",
	// "video", …) so the source row isn't mutated; FullMimetype keeps the rest.
	Mimetype     *string `json:"mimetype"`
	Path         string  `json:"path"`
	Type         string  `json:"type"`
	Thumb        string  `json:"thumb"`
	Banner       string  `json:"banner"`
	VideoUrl     string  `json:"video_url"`
	FullMimetype string  `json:"full_mimetype"`
}

type Uploader interface {
	Upload(c *gin.Context) (medias []map[string]any, err error)
	IsValidFile(file, arg string) error
	FilePath(filename string) (string, string, string)
	SaveFile(metadata map[string]any, data []byte) (map[string]any, error)
}

type uploader struct {
	Files []*multipart.FileHeader
	AwsS3 bool
}

func NewUploader() Uploader {
	s3 := false
	if config.IsAwsS3 {
		s3 = true
	}
	return &uploader{AwsS3: s3}
}

func (cmp *uploader) Upload(c *gin.Context) (medias []map[string]any, err error) {
	ct := c.GetHeader("Content-Type")
	if !strings.HasPrefix(ct, "multipart/form-data") {
		return nil, errors.New(cons.UploadRequiresMultipart)
	}
	const MaxUploadMemory = 100 << 20
	if err := c.Request.ParseMultipartForm(MaxUploadMemory); err != nil {
		return nil, fmt.Errorf("multipart form parse error: %w", err)
	}
	form, err := c.MultipartForm()
	if err != nil {
		return nil, fmt.Errorf("multipart form parse error: %w", err)
	}
	files := form.File["file"]
	if len(files) == 0 {
		return nil, errors.New(cons.NoFilesFound)
	}
	//	config.Log.Infof("Upload: received %d file(s) for processing", len(files))
	for i, file := range files {
		// config.Log.Infof("Upload[%d]: filename=%q size=%d", i, file.Filename, file.Size)
		ext := c.Query("extensions")
		if vErr := cmp.IsValidFile(file.Filename, ext); vErr != nil && !lib.IsEmpty(ext) {
			config.Log.Errorf("Upload[%d]: invalid file %q: %v", i, file.Filename, vErr)
			return nil, vErr
		}
		fp, oErr := file.Open()
		if oErr != nil {
			config.Log.Errorf("Upload[%d]: file.Open() failed for %q: %v", i, file.Filename, oErr)
			return nil, oErr
		}
		var path string
		if cmp.AwsS3 {
			// Note: UPLOAD MULTIPLE IMAGE WITH MULTIPLE SIZE. LIKE 1200x900 & 500x600
			path, err = cmp._s3upload(fp, file)
		} else {
			path, err = cmp._upload(c, file)
		}
		fp.Close()
		if err != nil {
			config.Log.Errorf("Upload[%d]: write failed for %q: %v", i, file.Filename, err)
			return nil, err
		}
		transform, sErr := cmp.saveToMedia(file, path)
		if sErr != nil {
			config.Log.Errorf("Upload[%d]: saveToMedia failed for %q: %v", i, file.Filename, sErr)
			return nil, sErr
		}
		medias = append(medias, transformMap(transform))
	}
	//	config.Log.Infof("Upload: completed, %d media row(s) returned", len(medias))
	return medias, nil
}

// Transform builds the API view of a core_media row: public path, thumb and
// banner URLs for images, and video_url for videos.
func (m *CoreMedia) Transform() *TransformCoreMedia {
	if m == nil {
		return nil
	}
	transform := TransformCoreMedia{
		CoreMedia:    m,
		Mimetype:     m.Mimetype,
		FullMimetype: m.GetMimetype(),
	}
	if p := m.GetPath(); p != "" {
		transform.Path = fmt.Sprintf("%s/%s", storagePath(), p)
	}

	log.Printf("transform.Path: %v\n", transform.Path)

	full := m.GetMimetype()
	if full == "" {
		return &transform
	}
	kind, subtype, _ := strings.Cut(full, "/")
	transform.Mimetype = &kind
	transform.Type = kind

	switch {
	case lib.InArray(subtype, []string{"webp", "svg", "svg+xml"}):
		transform.Thumb = transform.Path
		transform.Banner = transform.Path
	case kind == cons.CORE_MEDIA_MIME_TYPE_IMAGE && lib.InArray(subtype, []string{"png", "jpg", "jpeg", "gif"}):
		thumbWidth := lib.ToInt(os.Getenv("THUMB_WIDTH"))
		thumbHeight := lib.ToInt(os.Getenv("THUMB_HEIGHT"))
		bannerWidth := lib.ToInt(os.Getenv("BANNER_WIDTH"))
		bannerHeight := lib.ToInt(os.Getenv("BANNER_HEIGHT"))
		transform.Thumb = mediaimage.Thumb(m.GetPath(), thumbWidth, thumbHeight)
		transform.Banner = mediaimage.Thumb(m.GetPath(), bannerWidth, bannerHeight)
	}
	if kind == cons.CORE_MEDIA_MIME_TYPE_VIDEO {
		transform.VideoUrl = transform.Path
	}
	return &transform
}

// transformMap applies CoreMedia.Transform to a core_media row returned as a
// column map, so Upload responses carry the same path/thumb/banner/video_url/
// mimetype/full_mimetype/type fields as a transformed CoreMedia.
func transformMap(media map[string]any) map[string]any {
	if media == nil {
		return nil
	}
	path := columnString(media["path"])
	mimetype := columnString(media["mimetype"])
	t := (&CoreMedia{Path: &path, Mimetype: &mimetype}).Transform()

	media["path"] = t.Path
	media["mimetype"] = t.Mimetype
	media["full_mimetype"] = t.FullMimetype
	media["type"] = t.Type
	media["thumb"] = t.Thumb
	media["banner"] = t.Banner
	media["video_url"] = t.VideoUrl
	return media
}

// columnString reads a raw-scanned column value; MySQL text columns can come
// back as []byte, which lib.ToString would format as "[104 116 ...]".
func columnString(v any) string {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return lib.ToString(v)
}

// SaveFile writes a streamed upload to local storage and records it in
// core_media. metadata uses the lowercase keys "filename", "size" and
// "mimetype" (the gen.Metadata JSON names); "path" is set here.
func (cmp *uploader) SaveFile(metadata map[string]any, data []byte) (map[string]any, error) {
	if metadata == nil || lib.IsEmpty(metadata["filename"]) {
		return nil, errors.New(cons.NoFilesFound)
	}
	// Ensure the directory exists
	full_path, dir, newFileName := cmp.FilePath(lib.ToString(metadata["filename"]))
	if _, err := os.Stat(full_path); os.IsNotExist(err) {
		if err = os.MkdirAll(full_path, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create directory: %v", err)
		}
	}
	dst := full_path + newFileName
	metadata["path"] = dir + newFileName
	// Write data to the file
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write file: %v", err)
	}
	media, err := cmp._saveMedia(metadata)
	if err != nil {
		return nil, err
	}
	return transformMap(media), nil
}

func (cmp *uploader) FilePath(filename string) (string, string, string) {
	UPLOADROOT := ""
	if !cmp.AwsS3 {
		UPLOADROOT = os.Getenv("ROOT") + "/"
	}
	baseName := uuid.New().String()
	newFileName := baseName + filepath.Ext(filename)
	dir := lib.ChunkSplit(lib.Substr(baseName, 0, 8), 1, "/")
	fullPath := UPLOADROOT + "uploads/storage/" + dir
	return fullPath, dir, newFileName
}

// _saveMedia records a file written by SaveFile (streamed uploads).
func (cmp *uploader) _saveMedia(file map[string]any) (map[string]any, error) {
	return cmp._insertMedia(lib.ToString(file["filename"]), lib.ToInt32(file["size"]), lib.ToString(file["path"]), lib.ToString(file["mimetype"]))
}

// saveToMedia records a multipart upload once it has been written to disk/S3.
func (cmp *uploader) saveToMedia(file *multipart.FileHeader, path string) (map[string]any, error) {
	mimetype := strings.Join(file.Header["Content-Type"], "")
	return cmp._insertMedia(file.Filename, int32(file.Size), path, mimetype)
}

// _insertMedia inserts one core_media row (with a fresh guid) and returns it
// as a column map.
func (cmp *uploader) _insertMedia(filename string, filesize int32, path, mimetype string) (map[string]any, error) {
	now := time.Now().Unix()
	guid := uuid.NewString()
	sql := "INSERT INTO `core_media` (`guid`, `filename`, `filesize`, `path`, `mimetype`, `created_at`, `updated_at`) VALUES (?, ?, ?, ?, ?, ?, ?)"
	if err := activerecord.Exec(sql, guid, filename, filesize, path, mimetype, now, now).Error; err != nil {
		return nil, err
	}
	// Read the row back by its unique guid. LAST_INSERT_ID() isn't safe here:
	// Exec and the next query can run on different pooled connections.
	var media map[string]any
	sql = "SELECT * FROM `core_media` WHERE `guid` = ? LIMIT 1"
	if err := activerecord.Raw(sql, guid).Scan(&media).Error; err != nil {
		return nil, err
	}
	if media == nil {
		return nil, errors.New(cons.RecordNotFound)
	}
	return media, nil
}

func (cmp *uploader) IsValidFile(file, arg string) error {
	if lib.IsEmpty(arg) {
		return nil
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(file)), ".")
	exts := lib.Explode(",", config.GetString(arg))
	if ext == "" || !lib.InArray(ext, exts) {
		return errors.New(cons.UnSupportedFileFormat)
	}
	return nil
}

// Helper function to upload file to Google Cloud Storage
func (cmp *uploader) UploadToGCS(bucketName, filePath string, data []byte) (string, error) {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create GCS client: %v", err)
	}
	defer client.Close()
	bucket := client.Bucket(bucketName)
	writer := bucket.Object(filePath).NewWriter(ctx)
	writer.ContentType = "application/octet-stream" // Adjust based on file type
	writer.ACL = []storage.ACLRule{{Entity: storage.AllUsers, Role: storage.RoleReader}}

	if _, err := writer.Write(data); err != nil {
		return "", fmt.Errorf("failed to write to GCS: %v", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close GCS writer: %v", err)
	}

	// Construct the public URL of the uploaded file
	publicURL := fmt.Sprintf("https://storage.googleapis.com/%s/%s", bucketName, filePath)
	return publicURL, nil
}

// UploadFileToS3 saves a file to aws bucket and returns the url to the file and an error if there's any
func (cmp *uploader) _s3upload(file multipart.File, fileHeader *multipart.FileHeader) (string, error) {
	// get the file size and read
	// the file content into a buffer

	buffer, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	full_path, dir, newFileName := cmp.FilePath(fileHeader.Filename)

	// create a unique file name for the file
	fileName := full_path + newFileName
	baseName := dir + newFileName
	// cfg.Log.Println("newFileName: ", newFileName)
	// cfg.Log.Println("dir: ", dir)
	// cfg.Log.Println("fileName: ", fileName)
	// cfg.Log.Println("base file name: ", baseName)
	bucketName := os.Getenv("S3_BUCKET")

	// cfg.Log.Println("bucket_name:", bucket_name)
	// config settings: this is where you choose the bucket,
	// filename, content-type and storage class of the file
	// you're uploading

	/* New code */

	// Get the width and height of the image
	width, height, err := cmp._imageDimensions(bytes.NewReader(buffer))
	if err != nil {
		// Not a decodable image (video, pdf, svg, …): store the original as-is.
		_, err = config.AwsS3.PutObject(context.TODO(), &s3.PutObjectInput{
			Bucket:               aws.String(bucketName),
			Key:                  aws.String(fileName),
			ACL:                  types.ObjectCannedACLPublicRead,
			Body:                 bytes.NewReader(buffer),
			ContentLength:        aws.Int64(int64(len(buffer))),
			ContentType:          aws.String(http.DetectContentType(buffer)),
			ServerSideEncryption: types.ServerSideEncryptionAes256,
			StorageClass:         types.StorageClass("INTELLIGENT_TIERING"),
		})
		if err != nil {
			return "", err
		}
		return baseName, nil
	}
	var fileSizes []map[string]any
	if width > 0 && height > 0 {
		fileSizes = append(fileSizes, map[string]any{"w": width, "h": height, "f": fileName})
	}
	// Cache keys follow components/image.Thumb's "<name>-<w>-<h>.<ext>" naming
	// so thumb and banner don't overwrite each other.
	cacheKey := func(w, h int) string {
		ext := filepath.Ext(fileName)
		name := strings.Replace(strings.TrimSuffix(fileName, ext), "storage", "cache", 1)
		return fmt.Sprintf("%s-%d-%d%s", name, w, h, ext)
	}
	thumbWidth := lib.ToInt(os.Getenv("THUMB_WIDTH"))
	thumbHeight := lib.ToInt(os.Getenv("THUMB_HEIGHT"))
	if thumbWidth > 0 && thumbHeight > 0 {
		fileSizes = append(fileSizes, map[string]any{"w": thumbWidth, "h": thumbHeight, "f": cacheKey(thumbWidth, thumbHeight)})
	}
	bannerWidth := lib.ToInt(os.Getenv("BANNER_WIDTH"))
	bannerHeight := lib.ToInt(os.Getenv("BANNER_HEIGHT"))
	if bannerWidth > 0 && bannerHeight > 0 {
		fileSizes = append(fileSizes, map[string]any{"w": bannerWidth, "h": bannerHeight, "f": cacheKey(bannerWidth, bannerHeight)})
	}

	// cfg.Log.Println("fileSizes:", fileSizes)

	for _, s := range fileSizes {
		// Resize the image
		w := lib.ToInt(fmt.Sprintf("%v", s["w"]))
		h := lib.ToInt(fmt.Sprintf("%v", s["h"]))
		fileName := fmt.Sprintf("%v", s["f"])

		// cfg.Log.Println("w:", w)
		// cfg.Log.Println("h:", h)
		resizedImage, err := cmp.resizeImage(buffer, w, h) // Specify the desired width and height
		if err != nil {
			return "", err
		}

		// Convert the resized image to bytes
		var resizedBuffer bytes.Buffer
		if err = cmp._encodeImage(&resizedBuffer, resizedImage, fileHeader.Filename); err != nil {
			return "", err
		}

		// Upload the resized image to AWS S3
		_, err = config.AwsS3.PutObject(context.TODO(), &s3.PutObjectInput{
			Bucket:               aws.String(bucketName),
			Key:                  aws.String(fileName),
			ACL:                  types.ObjectCannedACLPublicRead,
			Body:                 bytes.NewReader(resizedBuffer.Bytes()),
			ContentLength:        aws.Int64(int64(resizedBuffer.Len())),
			ContentType:          aws.String(http.DetectContentType(resizedBuffer.Bytes())),
			ServerSideEncryption: types.ServerSideEncryptionAes256,
			StorageClass:         types.StorageClass("INTELLIGENT_TIERING"),
		})
		// Upload the resized image to AWS S3
		if err != nil {
			return "", err
		}
	}

	/* New code end */

	return baseName, nil
}

func (cmp *uploader) _imageDimensions(reader io.Reader) (int, int, error) {
	img, _, err := image.DecodeConfig(reader)
	if err != nil {
		return 0, 0, err
	}
	return img.Width, img.Height, nil
}

func (cmp *uploader) resizeImage(data []byte, width, height int) (image.Image, error) {
	// cfg.Log.Println("--- resizeImage ---")
	// Decode the image
	img, _, err := image.Decode(bytes.NewReader(data))
	// cfg.Log.Println("img:", img)
	if err != nil {
		return nil, err
	}

	// Resize the image
	resizedImage := resize.Resize(uint(width), uint(height), img, resize.Lanczos3)
	// cfg.Log.Println("resizedImage:", resizedImage)
	return resizedImage, nil
}

func (cmp *uploader) _upload(c *gin.Context, file *multipart.FileHeader) (path string, err error) {
	full_path, dir, newFileName := cmp.FilePath(file.Filename)
	if _, err = os.Stat(full_path); os.IsNotExist(err) {
		err = os.MkdirAll(full_path, 0o755)
		if err != nil {
			return
		}
	}
	dst := full_path + newFileName
	path = dir + newFileName
	if err = c.SaveUploadedFile(file, dst); err != nil {
		return
	}
	return path, nil
}

func (cmp *uploader) _encodeImage(w io.Writer, img image.Image, fileName string) error {
	// Detect the image format based on the file extension
	ext := filepath.Ext(fileName)
	switch ext {
	case ".jpeg", ".jpg":
		return jpeg.Encode(w, img, nil)
	case ".png":
		return png.Encode(w, img)
	case ".gif":
		return gif.Encode(w, img, nil)
	// Add more cases as needed for other formats
	default:
		return jpeg.Encode(w, img, nil)
		// return fmt.Errorf("unsupported image format: %s", ext)
	}
}

func UploadStreamImage(w http.ResponseWriter, r *http.Request) {
	file, handler, err := r.FormFile("file")
	if err != nil {
		// cfg.Log.Debugf(" error on upload ", err.Error(), w)
		fmt.Println("Error Uploading Image")
		return
	}
	defer file.Close()

	// Suggestion: You can calculate the file size from bytes.
	// instead getting it from form; since after resize image will change
	imgSize, err := strconv.Atoi(r.FormValue("imgsize"))
	if err != nil {
		println("Error converting to integer")
		// cfg.Log.Debugf("Error converting to integer", err.Error(), w)
		return
	}

	img, err := imaging.Decode(file)
	if err != nil {
		print("Imaging Open error")
		// cfg.Log.Debugf("Error decoding", err.Error(), w)
		return
	}

	b := img.Bounds()
	heightImg := b.Max.Y
	widthImg := b.Max.X

	// resize image
	height := int(float64(heightImg) * .23)
	width := int(float64(widthImg) * .23)

	if imgSize < 401 {
		height = int(float64(heightImg) * 1)
		width = int(float64(widthImg) * 1)

	} else if imgSize >= 401 && imgSize < 900 {
		height = int(float64(heightImg) * .68)
		width = int(float64(widthImg) * .68)
		println("Middle Image")
	} else if imgSize >= 900 && imgSize < 1300 {
		height = int(float64(heightImg) * .45)
		width = int(float64(widthImg) * .45)
	} else if imgSize >= 1301 && imgSize < 1700 {
		height = int(float64(heightImg) * .40)
		width = int(float64(widthImg) * .40)
	}
	new_img := imaging.Resize(img, width, height, imaging.Lanczos)
	// end resize

	var buf bytes.Buffer
	err = imaging.Encode(&buf, new_img, imaging.JPEG)
	if err != nil {
		log.Println(err)
		// cfg.Log.Debugf(" error encoding file", err.Error(), w)
		return
	}
	bucketName := os.Getenv("S3_BUCKET")
	// fileType := http.DetectContentType(buf.Bytes())
	// fileSize := buf.Len()
	path := handler.Filename

	resp, err := config.AwsS3.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(path),
		Body:   bytes.NewReader(buf.Bytes()),
		ACL:    types.ObjectCannedACLPublicRead,
	})
	if err != nil {
		fmt.Printf("bad response: %s", err)
		// cfg.Log.Debugf("error in putObject", err.Error(), w)
		return
	}

	fmt.Println("Done", resp)
}

func UploadImage() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		header, err := c.FormFile("image")
		if err != nil {
			c.String(http.StatusBadRequest, "failed to read image: %v", err)
			return
		}

		src, err := header.Open()
		if err != nil {
			c.String(http.StatusInternalServerError, "failed to open file: %v", err)
			return
		}
		defer src.Close()

		data, err := io.ReadAll(src)
		if err != nil {
			c.String(http.StatusInternalServerError, "failed to read image bytes: %v", err)
			return
		}

		contentType := http.DetectContentType(data)
		bucketName := os.Getenv("S3_BUCKET")
		key := "photo"

		_, err = config.AwsS3.PutObject(ctx, &s3.PutObjectInput{
			Bucket:        aws.String(bucketName),
			Key:           aws.String(key),
			Body:          bytes.NewReader(data),
			ContentLength: aws.Int64(int64(len(data))),
			ContentType:   aws.String(contentType),
			ACL:           types.ObjectCannedACLPublicRead,
		})
		if err != nil {
			c.String(http.StatusInternalServerError, "failed to upload image: %v", err)
			return
		}

		c.String(http.StatusOK, "Upload successful")
	}
}
