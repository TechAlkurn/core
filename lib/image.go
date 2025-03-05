package lib

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
)

func Thumb(file string, width int, height int) string {
	env_cache := os.Getenv("cache")
	env_image := os.Getenv("image")
	env_storage := os.Getenv("storage")
	storage_path := os.Getenv("storagePath")

	thumbs := strings.Split(file, ".")
	thumb := fmt.Sprintf("%s-%d-%d.%s", thumbs[0], width, height, thumbs[len(thumbs)-1])
	cache_thumb := fmt.Sprintf("%s/%s", env_cache, thumb)
	image_thumb := fmt.Sprintf("%s/%s", env_image, thumb)
	org_image_thumb := fmt.Sprintf("%s/%s", storage_path, thumb)
	file = fmt.Sprintf("%s/%s", env_storage, file)

	if FileIsNotExist(file) {
		thumbs = strings.Split("default.png", ".")
		thumb = fmt.Sprintf("%s-%d-%d.%s", thumbs[0], width, height, thumbs[len(thumbs)-1])
		cache_thumb = fmt.Sprintf("%s/%s", env_cache, thumb)
		image_thumb = fmt.Sprintf("%s/%s", env_image, thumb)
		org_image_thumb = fmt.Sprintf("%s/%s", storage_path, thumb)
		file = fmt.Sprintf("%s/%s", env_storage, "default.png")
	}

	dir := filepath.Dir(cache_thumb)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0o755)
		if err != nil {
			// log.Fatal(err)
			return org_image_thumb
		}
	}

	if FileIsExist(cache_thumb) {
		return image_thumb
	}

	// load images and make 100x100 thumbnails of them
	var thumbnail image.Image
	img, err := imaging.Open(file)
	if err != nil {
		// log.Fatal(err)
		return org_image_thumb
	}
	thumbnail = imaging.Thumbnail(img, width, height, imaging.CatmullRom)
	// create a new blank image
	dst := imaging.New(width, height, color.NRGBA{0, 0, 0, 0})
	// paste thumbnails into the new image side by side
	dst = imaging.Paste(dst, thumbnail, image.Pt(0, 0))

	// save the combined image to file
	err = imaging.Save(dst, cache_thumb)
	if err != nil {
		// log.Fatal(err)
		return org_image_thumb
	}
	return image_thumb
}

func ThumbS3(originalURL string, width int, height int) (string, string) {
	originalURL = fmt.Sprintf("uploads/storage/%s", originalURL)
	cacheKey := generateCacheKey(originalURL, width, height)
	thumb := fmt.Sprintf("%s/%s", os.Getenv("S3_CDN_ENDPOINT"), cacheKey)
	return cacheKey, thumb
}

func GenerateThumb(originalURL string, width int, height int) string {
	_, thumb := ThumbS3(originalURL, width, height)
	return thumb
}

func generateCacheKey(originalURL string, width, height int) string {
	// Extract the filename from the URL
	chunks := strings.Split(originalURL, "/")
	basename := chunks[len(chunks)-1]
	base := strings.Split(basename, ".")

	// Generate a unique cache key based on the filename and dimensions
	newbasename := fmt.Sprintf("%s-%dx%d.%s", base[0], width, height, base[1])
	cacheURL := strings.Replace(originalURL, "storage", "cache", 1)
	cacheURL = strings.Replace(cacheURL, basename, newbasename, 1)
	// config.Log.Println("cacheURL:", cacheURL)
	return cacheURL
}

/*
func _thumbS3(originalURL string, width int, height int) string {
	cacheKey, _ := ThumbS3(originalURL, width, height)
	// cacheKey := fmt.Sprintf("uploads/storage/%s", originalURL)
	// config.Log.Println("originalURL:", originalURL)
	// Generate a unique cache key based on the original image URL and dimensions
	// cacheKey := generateCacheKey(originalURL, width, height)
	// config.Log.Println("cacheKey:", cacheKey)

	// Check if the resized image is already cached
	if isCached(cacheKey) {
		// Serve the cached image directly
		// config.Log.Println("cache DATA Key:", cacheKey)
		// config.Log.Println("cacheKey:", cacheKey)
		return cacheKey
	}
	// config.Log.Println("cacheKey2:", cacheKey)
	// If not cached, download the original image from DigitalOcean Spaces
	data, err := downloadFromS3(originalURL)
	if err != nil {
		config.Log.Debugf("Failed to download original image : %v", err)
		return ""
	}

	// Decode the original image
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		config.Log.Debugf("Failed to decode original image : %v", err)
		return ""
	}

	// Resize the image
	resizedImage := resize.Resize(uint(width), uint(height), img, resize.Lanczos3)
	// Convert the resized image to bytes
	var resizedBuffer bytes.Buffer
	err = jpeg.Encode(&resizedBuffer, resizedImage, nil)
	if err != nil {
		config.Log.Debugf("Failed to encode resized image : %v", err)
		return ""
	}

	//  Upload the resized image to DigitalOcean Spaces
	if err = uploadToS3(cacheKey, &resizedBuffer); err != nil {
		// 		config.Log.Debugf("Failed to upload resized image : %v", err)
		return ""
	}
	return cacheKey
}



func isCached(cacheKey string) bool {
	// Check if the resized image is already cached in DigitalOcean Spaces
	// DigitalOcean Spaces bucket name
	bucketName := os.Getenv("S3_BUCKET")
	//	config.Log.Println("BUCKET cache URL:", cacheKey)
	// result, err := config.AwsS3.GetObject(&s3.GetObjectInput{Bucket: aws.String(bucketName), Key: aws.String(cacheKey)})
	// if err != nil {
	// 	config.Log.Debugf("Error: %v", err.Error())
	// 	return false
	// }
	// defer result.Body.Close()

	if _, err := config.MinioClient.StatObject(context.TODO(), bucketName, cacheKey, minio.StatObjectOptions{}); err == nil {
		return true
	}
	return false
}

func downloadFromS3(objectURL string) ([]byte, error) {
	// Download the original image from DigitalOcean Spaces
	bucketName := os.Getenv("S3_BUCKET")
	// cdn_endpoint := os.Getenv("S3_CDN_ENDPOINT")
	// objectURL = strings.TrimPrefix(objectURL, cdn_endpoint)
	// objectURL = strings.TrimPrefix(objectURL, "/")
	// config.Log.Println("objectURL:", objectURL)
	object, err := config.MinioClient.GetObject(context.TODO(), bucketName, objectURL, minio.GetObjectOptions{})
	// config.Log.Println("object:", object)
	if err != nil {
		return nil, err
	}
	defer object.Close()

	// Read the image data
	var data bytes.Buffer
	_, err = data.ReadFrom(object)
	//	config.Log.Println("err:", err)
	if err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

func uploadToS3(fileName string, buffer *bytes.Buffer) (err error) {
	// Upload the resized image to DigitalOcean Spaces
	// fileName = ChunkSplit(Substr(fileName, 0, 8), 1, "/")
	// config.Log.Println("file name:", fileName)
	bucketName := os.Getenv("S3_BUCKET")
	_, err = config.AwsS3.PutObject(&s3.PutObjectInput{
		Bucket:        aws.String(bucketName),
		Key:           aws.String(fileName),
		ACL:           aws.String("public-read"), // could be private if you want it to be access by only authorized users
		Body:          bytes.NewReader(buffer.Bytes()),
		ContentLength: aws.Int64(int64(buffer.Len())),
		ContentType:   aws.String(http.DetectContentType(buffer.Bytes())),
		// ContentDisposition:   aws.String("attachment"),
		ServerSideEncryption: aws.String("AES256"),
		StorageClass:         aws.String("INTELLIGENT_TIERING"),
	})
	return
}
*/
