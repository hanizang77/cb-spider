// Cloud Control Manager's Rest Runtime of CB-Spider.
// REST API implementation for S3Manager (minio-go based).
// by CB-Spider Team

package restruntime

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	cmrt "github.com/cloud-barista/cb-spider/api-runtime/common-runtime"
	"github.com/labstack/echo/v4"
	"github.com/minio/minio-go/v7"
)

// ---------- dummy struct for Swagger documentation ----------

// --------------- for Swagger doc (minio.BucketInfo)
type S3BucketInfo struct {
	Name         string    `json:"Name"`
	BucketRegion string    `json:"BucketRegion,omitempty"`
	CreationDate time.Time `json:"CreationDate"`
}

// --------------- for Swagger doc (minio.ObjectInfo)
type S3ObjectInfo struct {
	ETag              string              `json:"ETag"`
	Key               string              `json:"Key"`
	LastModified      time.Time           `json:"LastModified"`
	Size              int64               `json:"Size"`
	ContentType       string              `json:"ContentType"`
	Expires           time.Time           `json:"Expires"`
	Metadata          map[string][]string `json:"Metadata"`
	UserMetadata      map[string]string   `json:"UserMetadata,omitempty"`
	UserTags          map[string]string   `json:"UserTags,omitempty"`
	UserTagCount      int                 `json:"UserTagCount"`
	Owner             *S3Owner            `json:"Owner,omitempty"`
	Grant             []S3Grant           `json:"Grant,omitempty"`
	StorageClass      string              `json:"StorageClass"`
	IsLatest          bool                `json:"IsLatest"`
	IsDeleteMarker    bool                `json:"IsDeleteMarker"`
	VersionID         string              `json:"VersionID"`
	ReplicationStatus string              `json:"ReplicationStatus"`
	ReplicationReady  bool                `json:"ReplicationReady"`
	Expiration        time.Time           `json:"Expiration"`
	ExpirationRuleID  string              `json:"ExpirationRuleID"`
	NumVersions       int                 `json:"NumVersions"`
	Restore           *S3RestoreInfo      `json:"Restore,omitempty"`
	ChecksumCRC32     string              `json:"ChecksumCRC32"`
	ChecksumCRC32C    string              `json:"ChecksumCRC32C"`
	ChecksumSHA1      string              `json:"ChecksumSHA1"`
	ChecksumSHA256    string              `json:"ChecksumSHA256"`
	ChecksumCRC64NVME string              `json:"ChecksumCRC64NVME"`
	ChecksumMode      string              `json:"ChecksumMode"`
}

type S3Owner struct {
	DisplayName string `json:"DisplayName"`
	ID          string `json:"ID"`
}
type S3Grant struct {
	Grantee    interface{} `json:"Grantee"`
	Permission string      `json:"Permission"`
}
type S3RestoreInfo struct {
	OngoingRestore bool      `json:"OngoingRestore"`       // Whether the object is currently being restored
	ExpiryTime     time.Time `json:"ExpiryTime,omitempty"` // Optional, only if applicable
}

// --------------- for Swagger doc (minio.UploadInfo)
type S3UploadInfo struct {
	Bucket            string    `json:"Bucket"`
	Key               string    `json:"Key"`
	ETag              string    `json:"ETag"`
	Size              int64     `json:"Size"`
	LastModified      time.Time `json:"LastModified"`
	Location          string    `json:"Location"`
	VersionID         string    `json:"VersionID"`
	Expiration        time.Time `json:"Expiration"`
	ExpirationRuleID  string    `json:"ExpirationRuleID"`
	ChecksumCRC32     string    `json:"ChecksumCRC32"`
	ChecksumCRC32C    string    `json:"ChecksumCRC32C"`
	ChecksumSHA1      string    `json:"ChecksumSHA1"`
	ChecksumSHA256    string    `json:"ChecksumSHA256"`
	ChecksumCRC64NVME string    `json:"ChecksumCRC64NVME"`
	ChecksumMode      string    `json:"ChecksumMode"`
}

// --------------- for Swagger doc (minio.BooleanInfo)
type S3PresignedURL struct {
	PresignedURL string `json:"PresignedURL"`
}

// ---------- Common functions ----------

func getConnectionName(c echo.Context) (string, bool) {
	authHeader := c.Request().Header.Get("Authorization")
	if authHeader != "" && strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256") {
		accessKey, err := extractAccessKey(authHeader)
		if err == nil && accessKey != "" {
			cblog.Debugf("S3 API request detected with AccessKey: %s", accessKey)
			return accessKey, true
		}
	}

	conn := c.QueryParam("ConnectionName")
	if conn != "" {
		cblog.Debugf("CB-Spider API request with ConnectionName: %s", conn)
		return conn, false
	}

	// Check custom header for AdminWeb
	headerConn := c.Request().Header.Get("X-Connection-Name")
	if headerConn != "" {
		cblog.Debugf("AdminWeb request with X-Connection-Name: %s", headerConn)
		return headerConn, false
	}

	cblog.Debug("No connection name found in request")
	return "", false
}

func extractAccessKey(authHeader string) (string, error) {
	const prefix = "AWS4-HMAC-SHA256 "
	if !strings.HasPrefix(authHeader, prefix) {
		return "", errors.New("invalid Authorization header prefix")
	}

	parts := strings.Split(authHeader[len(prefix):], ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "Credential=") {
			credValue := strings.TrimPrefix(part, "Credential=")
			segments := strings.Split(credValue, "/")
			if len(segments) < 1 {
				return "", errors.New("invalid Credential format")
			}
			return segments[0], nil
		}
	}
	return "", errors.New("Credential field not found")
}

// S3 Error Response
type S3Error struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource"`
	RequestId string   `xml:"RequestId"`
}

func returnS3Error(c echo.Context, statusCode int, errorCode string, message string, resource string) error {
	requestId := fmt.Sprintf("%d", time.Now().Unix())
	c.Response().Header().Set("x-amz-request-id", requestId)
	c.Response().Header().Set("x-amz-id-2", requestId)

	s3Error := S3Error{
		Code:      errorCode,
		Message:   message,
		Resource:  resource,
		RequestId: requestId,
	}

	xmlData, err := xml.Marshal(s3Error)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Internal Server Error")
	}

	fullXML := append([]byte(xml.Header), xmlData...)
	return c.Blob(statusCode, "application/xml", fullXML)
}

func addS3Headers(c echo.Context) {
	requestId := fmt.Sprintf("%d", time.Now().Unix())
	c.Response().Header().Set("x-amz-request-id", requestId)
	c.Response().Header().Set("x-amz-id-2", requestId)
}

// ---------- XML Response Structures ----------

type ListAllMyBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	Xmlns   string   `xml:"xmlns,attr"`
	Owner   Owner    `xml:"Owner"`
	Buckets Buckets  `xml:"Buckets"`
}

type Owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

type Buckets struct {
	Bucket []Bucket `xml:"Bucket"`
}

type Bucket struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

type ListBucketResult struct {
	XMLName     xml.Name      `xml:"ListBucketResult"`
	Xmlns       string        `xml:"xmlns,attr"`
	Name        string        `xml:"Name"`
	Prefix      string        `xml:"Prefix"`
	Marker      string        `xml:"Marker"`
	MaxKeys     int           `xml:"MaxKeys"`
	IsTruncated bool          `xml:"IsTruncated"`
	Contents    []S3ObjectXML `xml:"Contents"`
}

type S3ObjectXML struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
	Owner        *Owner `xml:"Owner,omitempty"`
}

type CreateBucketConfiguration struct {
	XMLName            xml.Name `xml:"CreateBucketConfiguration"`
	LocationConstraint string   `xml:"LocationConstraint"`
}

// ---------- S3 Advanced Features XML Structures ----------

type CORSConfiguration struct {
	XMLName   xml.Name   `xml:"CORSConfiguration"`
	Xmlns     string     `xml:"xmlns,attr"`
	CORSRules []CORSRule `xml:"CORSRule"`
}

type CORSRule struct {
	AllowedOrigin []string `xml:"AllowedOrigin"`
	AllowedMethod []string `xml:"AllowedMethod"`
	AllowedHeader []string `xml:"AllowedHeader,omitempty"`
	ExposeHeader  []string `xml:"ExposeHeader,omitempty"`
	MaxAgeSeconds int      `xml:"MaxAgeSeconds,omitempty"`
}

type AccessControlPolicy struct {
	XMLName           xml.Name          `xml:"AccessControlPolicy"`
	Xmlns             string            `xml:"xmlns,attr"`
	Owner             Owner             `xml:"Owner"`
	AccessControlList AccessControlList `xml:"AccessControlList"`
}

type AccessControlList struct {
	Grant []Grant `xml:"Grant"`
}

type Grant struct {
	Grantee    Grantee `xml:"Grantee"`
	Permission string  `xml:"Permission"`
}

type Grantee struct {
	XMLName      xml.Name `xml:"Grantee"`
	Type         string   `xml:"type,attr"`
	ID           string   `xml:"ID,omitempty"`
	DisplayName  string   `xml:"DisplayName,omitempty"`
	EmailAddress string   `xml:"EmailAddress,omitempty"`
	URI          string   `xml:"URI,omitempty"`
}

type ListVersionsResult struct {
	XMLName             xml.Name        `xml:"ListVersionsResult"`
	Xmlns               string          `xml:"xmlns,attr"`
	Name                string          `xml:"Name"`
	Prefix              string          `xml:"Prefix"`
	KeyMarker           string          `xml:"KeyMarker"`
	VersionIdMarker     string          `xml:"VersionIdMarker"`
	NextKeyMarker       string          `xml:"NextKeyMarker"`
	NextVersionIdMarker string          `xml:"NextVersionIdMarker"`
	MaxKeys             int             `xml:"MaxKeys"`
	IsTruncated         bool            `xml:"IsTruncated"`
	Versions            []ObjectVersion `xml:"Version"`
	DeleteMarkers       []DeleteMarker  `xml:"DeleteMarker"`
}

type ObjectVersion struct {
	Key          string `xml:"Key"`
	VersionId    string `xml:"VersionId"`
	IsLatest     bool   `xml:"IsLatest"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
	Owner        *Owner `xml:"Owner,omitempty"`
}

type DeleteMarker struct {
	Key          string `xml:"Key"`
	VersionId    string `xml:"VersionId"`
	IsLatest     bool   `xml:"IsLatest"`
	LastModified string `xml:"LastModified"`
	Owner        *Owner `xml:"Owner,omitempty"`
}

type VersioningConfiguration struct {
	XMLName xml.Name `xml:"VersioningConfiguration"`
	Status  string   `xml:"Status"`
}

func getBucketVersioning(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucketName := c.Param("Name")
	bucketName = strings.TrimSuffix(bucketName, "/")

	status, err := cmrt.GetVersioning(conn, bucketName)
	if err != nil {
		cblog.Errorf("Failed to get versioning status for bucket %s: %v", bucketName, err)

		_, bucketErr := cmrt.GetS3Bucket(conn, bucketName)
		if bucketErr != nil {
			errorCode := "NoSuchBucket"
			if strings.Contains(bucketErr.Error(), "not found") {
				return returnS3Error(c, http.StatusNotFound, errorCode, bucketErr.Error(), "/"+bucketName)
			}
			return returnS3Error(c, http.StatusInternalServerError, "InternalError", bucketErr.Error(), "/"+bucketName)
		}

		status = "Suspended"
	}

	resp := VersioningConfiguration{
		Status: status,
	}

	addS3Headers(c)
	xmlData, err := xml.Marshal(resp)
	if err != nil {
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+bucketName)
	}

	fullXML := append([]byte(xml.Header), xmlData...)
	return c.Blob(http.StatusOK, "application/xml", fullXML)
}

// putBucketVersioning sets the versioning configuration of a bucket
func putBucketVersioning(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucketName := c.Param("Name")
	bucketName = strings.TrimSuffix(bucketName, "/")

	cblog.Infof("putBucketVersioning called - Bucket: %s, Connection: %s", bucketName, conn)
	cblog.Infof("Request method: %s", c.Request().Method)
	cblog.Infof("Request URL: %s", c.Request().URL.String())
	cblog.Infof("Request Content-Length: %d", c.Request().ContentLength)
	cblog.Infof("Request Content-Type: %s", c.Request().Header.Get("Content-Type"))

	// Log all query parameters
	cblog.Infof("All query parameters: %v", c.QueryParams())

	// First, check if bucket exists
	_, err := cmrt.GetS3Bucket(conn, bucketName)
	if err != nil {
		cblog.Errorf("Bucket %s not found: %v", bucketName, err)
		if strings.Contains(err.Error(), "not found") {
			return returnS3Error(c, http.StatusNotFound, "NoSuchBucket",
				"The specified bucket does not exist", "/"+bucketName)
		}
		return returnS3Error(c, http.StatusInternalServerError, "InternalError",
			err.Error(), "/"+bucketName)
	}

	cblog.Infof("Bucket %s exists, proceeding with versioning configuration", bucketName)

	// Read and parse the XML body
	var config VersioningConfiguration
	if c.Request().ContentLength > 0 {
		bodyBytes, err := io.ReadAll(c.Request().Body)
		if err != nil {
			cblog.Errorf("Failed to read request body: %v", err)
			return returnS3Error(c, http.StatusBadRequest, "MalformedXML",
				"Error reading request body: "+err.Error(), "/"+bucketName)
		}

		cblog.Infof("Request body: %s", string(bodyBytes))

		if err := xml.Unmarshal(bodyBytes, &config); err != nil {
			cblog.Errorf("Failed to unmarshal XML: %v", err)
			return returnS3Error(c, http.StatusBadRequest, "MalformedXML",
				"Error parsing XML: "+err.Error(), "/"+bucketName)
		}
	} else {
		cblog.Error("No request body provided")
		return returnS3Error(c, http.StatusBadRequest, "MalformedXML",
			"Request body is required", "/"+bucketName)
	}

	cblog.Infof("Parsed versioning config - Status: %s", config.Status)

	// Validate the status
	if config.Status != "Enabled" && config.Status != "Suspended" {
		cblog.Errorf("Invalid versioning status: %s", config.Status)
		return returnS3Error(c, http.StatusBadRequest, "InvalidArgument",
			"Invalid versioning status: "+config.Status, "/"+bucketName)
	}

	// Apply the versioning configuration
	var versioningErr error
	if config.Status == "Enabled" {
		cblog.Infof("Enabling versioning for bucket: %s", bucketName)
		_, versioningErr = cmrt.EnableVersioning(conn, bucketName)
	} else if config.Status == "Suspended" {
		cblog.Infof("Suspending versioning for bucket: %s", bucketName)
		_, versioningErr = cmrt.SuspendVersioning(conn, bucketName)
	}

	if versioningErr != nil {
		cblog.Errorf("Failed to set versioning for bucket %s: %v", bucketName, versioningErr)
		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError
		if strings.Contains(versioningErr.Error(), "not found") {
			errorCode = "NoSuchBucket"
			statusCode = http.StatusNotFound
		} else if strings.Contains(versioningErr.Error(), "not implemented") {
			errorCode = "NotImplemented"
			statusCode = http.StatusNotImplemented
		}
		return returnS3Error(c, statusCode, errorCode, versioningErr.Error(), "/"+bucketName)
	}

	cblog.Infof("Verifying versioning status after setting to %s for bucket %s", config.Status, bucketName)
	actualStatus, verifyErr := cmrt.GetVersioning(conn, bucketName)
	if verifyErr != nil {
		cblog.Warnf("Failed to verify versioning status: %v", verifyErr)
	} else {
		cblog.Infof("Verification result: requested=%s, actual=%s", config.Status, actualStatus)
		if actualStatus != config.Status {
			cblog.Warnf("Versioning status mismatch: requested=%s, actual=%s", config.Status, actualStatus)
		}
	}

	cblog.Infof("Successfully set versioning to %s for bucket %s", config.Status, bucketName)
	addS3Headers(c)
	return c.NoContent(http.StatusOK)
}

// getBucketCORS returns the CORS configuration of a bucket
func getBucketCORS(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucketName := c.Param("Name")
	bucketName = strings.TrimSuffix(bucketName, "/")

	corsConfig, err := cmrt.GetS3BucketCORS(conn, bucketName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "NoSuchCORSConfiguration") {
			return returnS3Error(c, http.StatusNotFound, "NoSuchCORSConfiguration", "The CORS configuration does not exist", "/"+bucketName)
		}
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+bucketName)
	}

	// Convert minio CORS config to S3 XML format
	var corsRules []CORSRule
	for _, rule := range corsConfig.CORSRules {
		corsRules = append(corsRules, CORSRule{
			AllowedOrigin: rule.AllowedOrigin,
			AllowedMethod: rule.AllowedMethod,
			AllowedHeader: rule.AllowedHeader,
			ExposeHeader:  rule.ExposeHeader,
			MaxAgeSeconds: rule.MaxAgeSeconds,
		})
	}

	resp := CORSConfiguration{
		Xmlns:     "http://s3.amazonaws.com/doc/2006-03-01/",
		CORSRules: corsRules,
	}

	addS3Headers(c)
	xmlData, err := xml.Marshal(resp)
	if err != nil {
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+bucketName)
	}

	fullXML := append([]byte(xml.Header), xmlData...)
	return c.Blob(http.StatusOK, "application/xml", fullXML)
}

// putBucketCORS sets the CORS configuration of a bucket
func putBucketCORS(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucketName := c.Param("Name")
	bucketName = strings.TrimSuffix(bucketName, "/")

	var config CORSConfiguration
	if err := xml.NewDecoder(c.Request().Body).Decode(&config); err != nil {
		return returnS3Error(c, http.StatusBadRequest, "MalformedXML", err.Error(), "/"+bucketName)
	}

	if len(config.CORSRules) == 0 {
		return returnS3Error(c, http.StatusBadRequest, "InvalidRequest", "At least one CORS rule is required", "/"+bucketName)
	}

	// Use the first CORS rule for simplicity (CB-Spider limitation)
	rule := config.CORSRules[0]

	// Set default values if not provided
	if len(rule.AllowedOrigin) == 0 {
		rule.AllowedOrigin = []string{"*"}
	}
	if len(rule.AllowedMethod) == 0 {
		rule.AllowedMethod = []string{"GET", "PUT", "POST", "DELETE", "HEAD"}
	}
	if len(rule.AllowedHeader) == 0 {
		rule.AllowedHeader = []string{"*"}
	}
	if len(rule.ExposeHeader) == 0 {
		rule.ExposeHeader = []string{"ETag", "x-amz-server-side-encryption", "x-amz-request-id", "x-amz-id-2"}
	}
	if rule.MaxAgeSeconds == 0 {
		rule.MaxAgeSeconds = 3600
	}

	_, err := cmrt.SetS3BucketCORS(conn, bucketName, rule.AllowedOrigin, rule.AllowedMethod, rule.AllowedHeader, rule.ExposeHeader, rule.MaxAgeSeconds)
	if err != nil {
		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			errorCode = "NoSuchBucket"
			statusCode = http.StatusNotFound
		}
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucketName)
	}

	addS3Headers(c)
	return c.NoContent(http.StatusOK)
}

// deleteBucketCORS deletes the CORS configuration of a bucket
func deleteBucketCORS(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucketName := c.Param("Name")
	bucketName = strings.TrimSuffix(bucketName, "/")

	_, err := cmrt.DeleteS3BucketCORS(conn, bucketName)
	if err != nil {
		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			errorCode = "NoSuchBucket"
			statusCode = http.StatusNotFound
		}
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucketName)
	}

	addS3Headers(c)
	return c.NoContent(http.StatusNoContent)
}

// listObjectVersions lists all versions of objects in a bucket
func listObjectVersions(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucketName := c.Param("Name")
	bucketName = strings.TrimSuffix(bucketName, "/")

	cblog.Infof("listObjectVersions called - Bucket: %s, Connection: %s", bucketName, conn)

	prefix := c.QueryParam("prefix")
	if prefix == "" {
		prefix = c.QueryParam("Prefix")
	}
	cblog.Infof("Using prefix: '%s'", prefix)

	// First check if bucket exists
	_, err := cmrt.GetS3Bucket(conn, bucketName)
	if err != nil {
		cblog.Errorf("Bucket %s not found: %v", bucketName, err)
		errorCode := "NoSuchBucket"
		statusCode := http.StatusNotFound
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucketName)
	}

	result, err := cmrt.ListS3ObjectVersions(conn, bucketName, prefix)
	if err != nil {
		cblog.Errorf("Failed to list object versions in bucket %s: %v", bucketName, err)
		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			errorCode = "NoSuchBucket"
			statusCode = http.StatusNotFound
		} else if strings.Contains(err.Error(), "not implemented") || strings.Contains(err.Error(), "NotImplemented") {
			errorCode = "NotImplemented"
			statusCode = http.StatusNotImplemented
		}
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucketName)
	}

	cblog.Infof("Found %d object versions/delete markers in bucket %s", len(result), bucketName)

	var versions []ObjectVersion
	var deleteMarkers []DeleteMarker

	for _, obj := range result {
		if obj.IsDeleteMarker {
			cblog.Infof("Processing DELETE MARKER: Key=%s, VersionID=%s", obj.Key, obj.VersionID)

			// For DELETE MARKER, if Version ID is empty, use "null" as per AWS standard
			versionID := obj.VersionID
			if versionID == "" {
				versionID = "null"
				cblog.Infof("DELETE MARKER has empty version ID, using 'null': %s", obj.Key)
			}

			deleteMarkers = append(deleteMarkers, DeleteMarker{
				Key:          obj.Key,
				VersionId:    versionID,
				IsLatest:     obj.IsLatest,
				LastModified: obj.LastModified.UTC().Format(time.RFC3339),
				Owner: &Owner{
					ID:          conn,
					DisplayName: conn,
				},
			})
		} else {
			versions = append(versions, ObjectVersion{
				Key:          obj.Key,
				VersionId:    obj.VersionID,
				IsLatest:     obj.IsLatest,
				LastModified: obj.LastModified.UTC().Format(time.RFC3339),
				ETag:         strings.Trim(obj.ETag, "\""),
				Size:         obj.Size,
				StorageClass: "STANDARD",
				Owner: &Owner{
					ID:          conn,
					DisplayName: conn,
				},
			})
		}
	}

	resp := ListVersionsResult{
		Xmlns:         "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:          bucketName,
		Prefix:        prefix,
		MaxKeys:       1000,
		IsTruncated:   false,
		Versions:      versions,
		DeleteMarkers: deleteMarkers,
	}

	addS3Headers(c)
	xmlData, err := xml.Marshal(resp)
	if err != nil {
		cblog.Errorf("Failed to marshal XML: %v", err)
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+bucketName)
	}

	fullXML := append([]byte(xml.Header), xmlData...)
	return c.Blob(http.StatusOK, "application/xml", fullXML)
}

func CreateS3Bucket(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucketName := c.Param("Name")
	if bucketName == "" {
		return returnS3Error(c, http.StatusBadRequest, "InvalidBucketName", "Bucket name is required", "/")
	}

	// Get all query parameters for debugging
	queryParams := c.QueryParams()
	cblog.Infof("CreateS3Bucket called - Method: %s, Path: %s, Bucket: %s", c.Request().Method, c.Path(), bucketName)
	cblog.Infof("Query parameters: %v", queryParams)

	// Check individual query parameters - if any configuration params exist, redirect to GetS3Bucket
	versioning := c.QueryParam("versioning")
	cors := c.QueryParam("cors")
	policy := c.QueryParam("policy")
	location := c.QueryParam("location")
	versions := c.QueryParam("versions")

	cblog.Infof("Individual params - versioning: '%s', cors: '%s', policy: '%s', location: '%s', versions: '%s'", versioning, cors, policy, location, versions)

	// Check if this is a configuration request (any query parameter that indicates configuration)
	// Use QueryParams().Has() to check for parameter existence regardless of value
	if c.QueryParams().Has("versioning") || c.QueryParams().Has("cors") ||
		c.QueryParams().Has("policy") || c.QueryParams().Has("location") || c.QueryParams().Has("versions") {
		cblog.Infof("Detected bucket configuration request, redirecting to GetS3Bucket")
		return GetS3Bucket(c)
	}

	// Check for any other query parameters that might indicate this is not a bucket creation
	hasNonConnectionParams := false
	for key := range queryParams {
		// Skip ConnectionName as it's our internal parameter
		if key != "ConnectionName" {
			hasNonConnectionParams = true
			cblog.Infof("Found query parameter '%s', redirecting to GetS3Bucket for proper handling", key)
			break
		}
	}

	if hasNonConnectionParams {
		return GetS3Bucket(c)
	}

	// Only proceed with bucket creation if this is a pure PUT request without configuration query parameters
	if c.Request().Method != "PUT" {
		cblog.Infof("Non-PUT request, redirecting to GetS3Bucket")
		return GetS3Bucket(c)
	}

	var region string = "us-east-1"
	if c.Request().ContentLength > 0 {
		var config CreateBucketConfiguration
		if err := xml.NewDecoder(c.Request().Body).Decode(&config); err == nil {
			if config.LocationConstraint != "" {
				region = config.LocationConstraint
			}
		}
	}

	cblog.Infof("Proceeding with bucket creation: %s in region: %s", bucketName, region)

	_, err := cmrt.CreateS3Bucket(conn, bucketName)
	if err != nil {
		cblog.Errorf("Failed to create bucket %s: %v", bucketName, err)

		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError

		if strings.Contains(err.Error(), "already exists") {
			errorCode = "BucketAlreadyExists"
			statusCode = http.StatusConflict
		} else if strings.Contains(err.Error(), "already owned") {
			errorCode = "BucketAlreadyOwnedByYou"
			statusCode = http.StatusConflict
		}

		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucketName)
	}

	addS3Headers(c)
	c.Response().Header().Set("Location", "/"+bucketName)
	return c.NoContent(http.StatusOK)
}

func ListS3Buckets(c echo.Context) error {
	conn, _ := getConnectionName(c)

	cblog.Infof("ListS3Buckets called - conn: %s", conn)

	// If no connection name found, return error instead of empty response
	if conn == "" {
		return returnS3Error(c, http.StatusBadRequest, "MissingParameter", "ConnectionName parameter is required", "/")
	}

	result, err := cmrt.ListS3Buckets(conn)
	if err != nil {
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/")
	}

	var bucketElems []Bucket
	for _, b := range result {
		bucketElems = append(bucketElems, Bucket{
			Name:         b.Name,
			CreationDate: b.CreationDate.UTC().Format(time.RFC3339),
		})
	}

	resp := ListAllMyBucketsResult{
		Xmlns: "http://s3.amazonaws.com/doc/2006-03-01/",
		Owner: Owner{
			ID:          conn,
			DisplayName: conn,
		},
		Buckets: Buckets{Bucket: bucketElems},
	}

	// Generate XML response
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")

	if err := enc.Encode(resp); err != nil {
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/")
	}

	xmlContent := buf.Bytes()
	cblog.Debugf("Generated XML response: %s", string(xmlContent))

	addS3Headers(c)
	c.Response().Header().Set("Content-Type", "application/xml")
	c.Response().Header().Set("Content-Length", strconv.Itoa(len(xmlContent)))

	return c.Blob(http.StatusOK, "application/xml", xmlContent)
}

func GetS3Bucket(c echo.Context) error {
	conn, _ := getConnectionName(c)
	name := c.Param("Name")
	name = strings.TrimSuffix(name, "/")

	cblog.Infof("GetS3Bucket called - Method: %s, Path: %s, Bucket: %s", c.Request().Method, c.Path(), name)
	cblog.Infof("Query parameters: %v", c.QueryParams())

	// Handle PUT requests with specific query parameters
	if c.Request().Method == "PUT" {
		cblog.Infof("PUT request received for bucket: %s", name)

		// Check for versioning parameter - this parameter exists but may be empty
		if c.QueryParams().Has("versioning") {
			cblog.Infof("Handling PUT versioning for bucket: %s", name)
			return putBucketVersioning(c)
		}
		if c.QueryParams().Has("cors") {
			cblog.Infof("Handling PUT cors for bucket: %s", name)
			return putBucketCORS(c)
		}
		// Log all query parameters for debugging
		cblog.Infof("All query parameters: %v", c.QueryParams())

		// If PUT request has no matching query params, check if bucket exists
		// If bucket doesn't exist, this might be a creation request that was misrouted
		cblog.Infof("PUT request with no matching query params, checking if bucket exists")
		_, err := cmrt.GetS3Bucket(conn, name)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				// Bucket doesn't exist, this might be a creation request
				cblog.Infof("Bucket %s doesn't exist, this might be a creation request", name)
				return returnS3Error(c, http.StatusNotFound, "NoSuchBucket",
					"The specified bucket does not exist", "/"+name)
			}
			return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
		}

		// Bucket exists but no valid operation specified
		cblog.Errorf("PUT request for existing bucket %s with no valid operation. Query params: %v", name, c.QueryParams())
		return returnS3Error(c, http.StatusBadRequest, "InvalidRequest",
			"Invalid PUT request - no valid operation specified", "/"+name)
	}

	// Handle GET requests with specific query parameters
	if c.Request().Method == "GET" {
		if c.QueryParams().Has("location") {
			cblog.Infof("Handling GET location for bucket: %s", name)
			return getBucketLocation(c)
		}
		if c.QueryParams().Has("versioning") {
			cblog.Infof("Handling GET versioning for bucket: %s", name)
			return getBucketVersioning(c)
		}
		if c.QueryParams().Has("cors") {
			cblog.Infof("Handling GET cors for bucket: %s", name)
			return getBucketCORS(c)
		}
		if c.QueryParams().Has("versions") {
			cblog.Infof("Handling GET versions for bucket: %s", name)
			return listObjectVersions(c)
		}

		// If no special query parameters, this is a list objects request
		if !c.QueryParams().Has("versioning") &&
			!c.QueryParams().Has("policy") &&
			!c.QueryParams().Has("lifecycle") &&
			!c.QueryParams().Has("cors") &&
			!c.QueryParams().Has("versions") &&
			!c.QueryParams().Has("location") {
			cblog.Infof("No special query params, treating as list objects request for bucket: %s", name)
			c.SetParamNames("Name")
			c.SetParamValues(name)
			return ListS3Objects(c)
		}
	}

	// Handle DELETE requests with specific query parameters
	if c.Request().Method == "DELETE" {
		if c.QueryParams().Has("cors") {
			cblog.Infof("Handling DELETE cors for bucket: %s", name)
			return deleteBucketCORS(c)
		}

		// If no query parameters, this is likely a delete bucket request
		// but it should go to DeleteS3Bucket function instead
		cblog.Infof("DELETE request with no query params, redirecting to bucket deletion")
		return DeleteS3Bucket(c)
	}

	// Handle HEAD requests
	if c.Request().Method == "HEAD" {
		cblog.Infof("HEAD request for bucket: %s", name)
		_, err := cmrt.GetS3Bucket(conn, name)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return c.NoContent(http.StatusNotFound)
			}
			return c.NoContent(http.StatusForbidden)
		}
		addS3Headers(c)
		return c.NoContent(http.StatusOK)
	}

	// Default behavior - just check if bucket exists
	cblog.Infof("Default bucket existence check for: %s", name)
	_, err := cmrt.GetS3Bucket(conn, name)
	if err != nil {
		errorCode := "NoSuchBucket"
		if strings.Contains(err.Error(), "not found") {
			return returnS3Error(c, http.StatusNotFound, errorCode, err.Error(), "/"+name)
		}
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	return c.NoContent(http.StatusOK)
}

// getBucketLocation returns the location (region) of a bucket
func getBucketLocation(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucketName := c.Param("Name")
	bucketName = strings.TrimSuffix(bucketName, "/")

	bucketInfo, err := cmrt.GetS3Bucket(conn, bucketName)
	region := ""
	if err == nil && bucketInfo.BucketRegion != "" {
		region = bucketInfo.BucketRegion
	}

	type LocationConstraint struct {
		XMLName            xml.Name `xml:"LocationConstraint"`
		Xmlns              string   `xml:"xmlns,attr"`
		LocationConstraint string   `xml:",chardata"`
	}

	resp := LocationConstraint{
		Xmlns:              "http://s3.amazonaws.com/doc/2006-03-01/",
		LocationConstraint: region,
	}

	addS3Headers(c)

	xmlData, err := xml.Marshal(resp)
	if err != nil {
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+bucketName)
	}

	fullXML := append([]byte(xml.Header), xmlData...)
	return c.Blob(http.StatusOK, "application/xml", fullXML)
}

func DeleteS3Bucket(c echo.Context) error {
	conn, _ := getConnectionName(c)
	name := c.Param("Name")

	cblog.Infof("DeleteS3Bucket called - Bucket: %s, Connection: %s", name, conn)
	cblog.Infof("Request method: %s, URL: %s", c.Request().Method, c.Request().URL.String())
	cblog.Infof("Query parameters: %v", c.QueryParams())

	// Check if this is actually a configuration delete request
	if c.QueryParams().Has("cors") {
		cblog.Infof("CORS delete request detected, redirecting to GetS3Bucket")
		return GetS3Bucket(c)
	}
	if c.QueryParams().Has("policy") {
		cblog.Infof("Policy delete request detected, redirecting to GetS3Bucket")
		return GetS3Bucket(c)
	}

	// Check for force delete or force empty
	if c.QueryParams().Has("force") || c.Request().Header.Get("X-Force-Delete") != "" {
		cblog.Infof("Force delete requested for bucket %s", name)
		return ForceDeleteS3Bucket(c)
	}

	if c.QueryParams().Has("empty") || c.Request().Header.Get("X-Force-Empty") != "" {
		cblog.Infof("Force empty requested for bucket %s", name)
		return ForceEmptyS3Bucket(c)
	}

	// First, check if bucket exists
	_, err := cmrt.GetS3Bucket(conn, name)
	if err != nil {
		cblog.Errorf("Bucket %s not found: %v", name, err)
		if strings.Contains(err.Error(), "not found") {
			return returnS3Error(c, http.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
		}
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	cblog.Infof("Bucket %s exists, proceeding with deletion checks", name)

	// Check for regular objects first
	cblog.Infof("Checking for regular objects in bucket %s", name)
	objects, err := cmrt.ListS3Objects(conn, name, "")
	if err != nil {
		cblog.Errorf("Failed to list objects in bucket %s: %v", name, err)
		// Continue with deletion attempt even if listing fails
	} else {
		cblog.Infof("Found %d regular objects in bucket %s", len(objects), name)

		if len(objects) > 0 {
			cblog.Warnf("Bucket %s is not empty - contains %d objects", name, len(objects))
			return returnS3Error(c, http.StatusConflict, "BucketNotEmpty",
				fmt.Sprintf("The bucket you tried to delete is not empty. It contains %d objects. Use force=true parameter to force delete.", len(objects)),
				"/"+name)
		}
	}

	// For versioning-enabled buckets, check for object versions and delete markers
	cblog.Infof("Checking for object versions and delete markers in bucket %s", name)
	versions, err := cmrt.ListS3ObjectVersions(conn, name, "")
	if err != nil {
		cblog.Warnf("Failed to list object versions (bucket might not have versioning enabled): %v", err)
		// Continue - this is expected for non-versioning buckets
	} else {
		cblog.Infof("Found %d object versions/delete markers in bucket %s", len(versions), name)

		if len(versions) > 0 {
			cblog.Warnf("Bucket %s has %d object versions/delete markers", name, len(versions))

			// Log details of versions for debugging
			var deleteMarkers int
			var objectVersions int
			for i, version := range versions {
				if i < 5 { // Log first 5 for debugging
					cblog.Infof("Version %d: Key=%s, VersionID=%s, IsLatest=%t, IsDeleteMarker=%t",
						i+1, version.Key, version.VersionID, version.IsLatest, version.IsDeleteMarker)
				}
				if version.IsDeleteMarker {
					deleteMarkers++
				} else {
					objectVersions++
				}
			}
			cblog.Infof("Summary: %d object versions, %d delete markers", objectVersions, deleteMarkers)

			return returnS3Error(c, http.StatusConflict, "BucketNotEmpty",
				fmt.Sprintf("The bucket you tried to delete has %d object versions and %d delete markers. Use force=true parameter to force delete.", objectVersions+deleteMarkers),
				"/"+name)
		}
	}

	cblog.Infof("Bucket %s appears to be empty (no objects, versions, or delete markers), proceeding with deletion", name)

	// Attempt to delete the bucket
	success, err := cmrt.DeleteS3Bucket(conn, name)
	if err != nil {
		cblog.Errorf("Failed to delete bucket %s: %v", name, err)

		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError

		if strings.Contains(err.Error(), "not empty") || strings.Contains(err.Error(), "BucketNotEmpty") {
			errorCode = "BucketNotEmpty"
			statusCode = http.StatusConflict
		} else if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "NoSuchBucket") {
			errorCode = "NoSuchBucket"
			statusCode = http.StatusNotFound
		} else if strings.Contains(err.Error(), "access denied") || strings.Contains(err.Error(), "AccessDenied") {
			errorCode = "AccessDenied"
			statusCode = http.StatusForbidden
		} else if strings.Contains(err.Error(), "versioning") || strings.Contains(err.Error(), "delete marker") {
			errorCode = "BucketNotEmpty"
			statusCode = http.StatusConflict
		}

		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+name)
	}

	if !success {
		cblog.Errorf("Bucket deletion returned false for bucket %s", name)
		return returnS3Error(c, http.StatusInternalServerError, "InternalError",
			"Bucket deletion failed for unknown reason", "/"+name)
	}

	cblog.Infof("Successfully deleted bucket %s", name)
	addS3Headers(c)
	return c.NoContent(http.StatusNoContent)
}

func ListS3Objects(c echo.Context) error {
	cblog.Infof("ListS3Objects called - Path: %s, Method: %s", c.Path(), c.Request().Method)

	conn, _ := getConnectionName(c)
	var bucket string
	var prefix string
	var delimiter string

	bucket = c.Param("Name")
	if bucket == "" {
		bucket = c.Param("BucketName")
	}
	bucket = strings.TrimSuffix(bucket, "/")

	prefix = c.QueryParam("prefix")
	if prefix == "" {
		prefix = c.QueryParam("Prefix")
	}

	delimiter = c.QueryParam("delimiter")
	if delimiter == "" {
		delimiter = c.QueryParam("Delimiter")
	}

	cblog.Infof("S3 API - Bucket: %s, Prefix: '%s', Delimiter: '%s', Connection: %s", bucket, prefix, delimiter, conn)

	if bucket == "" {
		return returnS3Error(c, http.StatusBadRequest, "InvalidBucketName", "Bucket name is required", "/")
	}

	// First check if bucket exists
	_, err := cmrt.GetS3Bucket(conn, bucket)
	if err != nil {
		cblog.Errorf("Bucket %s not found: %v", bucket, err)
		errorCode := "NoSuchBucket"
		statusCode := http.StatusNotFound
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucket)
	}

	cblog.Infof("Bucket %s exists, listing objects", bucket)

	result, err := cmrt.ListS3Objects(conn, bucket, prefix)
	if err != nil {
		cblog.Errorf("Failed to list objects in bucket %s: %v", bucket, err)
		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			errorCode = "NoSuchBucket"
			statusCode = http.StatusNotFound
		}
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucket)
	}

	cblog.Infof("Found %d objects in bucket %s with prefix '%s'", len(result), bucket, prefix)

	// Log first few objects for debugging
	for i, obj := range result {
		if i < 5 { // Log first 5 objects
			cblog.Infof("Object %d: Key=%s, Size=%d, LastModified=%s", i+1, obj.Key, obj.Size, obj.LastModified)
		}
	}
	if len(result) > 5 {
		cblog.Infof("... and %d more objects", len(result)-5)
	}

	// Handle delimiter-based folder structure
	if delimiter == "/" {
		type CommonPrefix struct {
			Prefix string `xml:"Prefix"`
		}

		type ListBucketResultWithPrefix struct {
			XMLName        xml.Name       `xml:"ListBucketResult"`
			Xmlns          string         `xml:"xmlns,attr"`
			Name           string         `xml:"Name"`
			Prefix         string         `xml:"Prefix"`
			Delimiter      string         `xml:"Delimiter"`
			Marker         string         `xml:"Marker"`
			MaxKeys        int            `xml:"MaxKeys"`
			IsTruncated    bool           `xml:"IsTruncated"`
			Contents       []S3ObjectXML  `xml:"Contents"`
			CommonPrefixes []CommonPrefix `xml:"CommonPrefixes"`
		}

		var contents []S3ObjectXML
		commonPrefixMap := make(map[string]bool)

		cblog.Infof("Processing objects with delimiter '/' for folder structure")

		for _, obj := range result {
			objKey := obj.Key

			// Skip objects that don't start with the specified prefix
			if prefix != "" && !strings.HasPrefix(objKey, prefix) {
				continue
			}

			// Calculate relative key (remove prefix)
			relativeKey := objKey
			if prefix != "" {
				relativeKey = strings.TrimPrefix(objKey, prefix)
			}

			// Check if this object represents a folder
			if delimiterIndex := strings.Index(relativeKey, delimiter); delimiterIndex > 0 {
				// This is inside a subfolder, create a common prefix
				subPrefix := prefix + relativeKey[:delimiterIndex+1]
				commonPrefixMap[subPrefix] = true
				cblog.Debugf("Adding common prefix: %s", subPrefix)
			} else if relativeKey != "" {
				// This is a direct file (not in a subfolder)
				// Skip the prefix itself if it's a folder marker
				if !(strings.HasSuffix(objKey, "/") && objKey == prefix) {
					contents = append(contents, S3ObjectXML{
						Key:          objKey,
						LastModified: obj.LastModified.UTC().Format(time.RFC3339),
						ETag:         strings.Trim(obj.ETag, "\""),
						Size:         obj.Size,
						StorageClass: "STANDARD",
					})
					cblog.Debugf("Adding direct file: %s", objKey)
				}
			}
		}

		// Convert common prefix map to slice
		var commonPrefixes []CommonPrefix
		for prefixKey := range commonPrefixMap {
			commonPrefixes = append(commonPrefixes, CommonPrefix{Prefix: prefixKey})
		}

		cblog.Infof("Final result: %d files, %d folders", len(contents), len(commonPrefixes))

		resp := ListBucketResultWithPrefix{
			Xmlns:          "http://s3.amazonaws.com/doc/2006-03-01/",
			Name:           bucket,
			Prefix:         prefix,
			Delimiter:      delimiter,
			Marker:         "",
			MaxKeys:        1000,
			IsTruncated:    false,
			Contents:       contents,
			CommonPrefixes: commonPrefixes,
		}

		addS3Headers(c)

		xmlData, err := xml.Marshal(resp)
		if err != nil {
			return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+bucket)
		}

		fullXML := append([]byte(xml.Header), xmlData...)
		cblog.Debugf("Returning XML with %d objects and %d common prefixes", len(contents), len(commonPrefixes))

		// Log XML snippet for debugging
		xmlStr := string(fullXML)
		if len(xmlStr) > 1000 {
			cblog.Debugf("XML response preview: %s...", xmlStr[:1000])
		} else {
			cblog.Debugf("XML response: %s", xmlStr)
		}

		return c.Blob(http.StatusOK, "application/xml", fullXML)
	}

	// Default response without delimiter (flat list)
	cblog.Infof("Processing objects as flat list (no delimiter)")

	var contents []S3ObjectXML
	for _, o := range result {
		contents = append(contents, S3ObjectXML{
			Key:          o.Key,
			LastModified: o.LastModified.UTC().Format(time.RFC3339),
			ETag:         strings.Trim(o.ETag, "\""),
			Size:         o.Size,
			StorageClass: "STANDARD",
		})
	}

	resp := ListBucketResult{
		Xmlns:       "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:        bucket,
		Prefix:      prefix,
		Marker:      "",
		MaxKeys:     1000,
		IsTruncated: false,
		Contents:    contents,
	}

	addS3Headers(c)
	cblog.Debugf("Returning flat list with %d objects", len(contents))

	xmlData, err := xml.Marshal(resp)
	if err != nil {
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+bucket)
	}

	fullXML := append([]byte(xml.Header), xmlData...)

	// Log XML snippet for debugging
	xmlStr := string(fullXML)
	if len(xmlStr) > 1000 {
		cblog.Debugf("XML response preview: %s...", xmlStr[:1000])
	} else {
		cblog.Debugf("XML response: %s", xmlStr)
	}

	return c.Blob(http.StatusOK, "application/xml", fullXML)
}

func GetS3ObjectInfo(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucket := c.Param("BucketName")
	obj := c.Param("ObjectKey+")
	versionId := c.QueryParam("versionId")

	cblog.Infof("GetS3ObjectInfo - Bucket: %s, Object: %s, VersionId: %s", bucket, obj, versionId)

	var o *minio.ObjectInfo
	var err error

	if versionId != "" && versionId != "null" && versionId != "undefined" {
		cblog.Infof("Getting info for specific version: %s", versionId)
		o, err = cmrt.GetS3ObjectInfoWithVersion(conn, bucket, obj, versionId)
	} else {
		cblog.Infof("Getting info for latest version")
		o, err = cmrt.GetS3ObjectInfo(conn, bucket, obj)
	}

	if err != nil {
		cblog.Errorf("Failed to get object info: %v", err)
		errorCode := "NoSuchKey"
		statusCode := http.StatusNotFound
		if strings.Contains(err.Error(), "bucket") {
			errorCode = "NoSuchBucket"
		} else if strings.Contains(err.Error(), "version") {
			errorCode = "NoSuchVersion"
		}
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucket+"/"+obj)
	}

	if c.Request().Method == "HEAD" {
		addS3Headers(c)
		c.Response().Header().Set("Content-Type", o.ContentType)
		c.Response().Header().Set("Content-Length", strconv.FormatInt(o.Size, 10))
		c.Response().Header().Set("Last-Modified", o.LastModified.UTC().Format(http.TimeFormat))
		c.Response().Header().Set("ETag", o.ETag)
		if o.VersionID != "" {
			c.Response().Header().Set("x-amz-version-id", o.VersionID)
		} else if versionId != "" && versionId != "null" && versionId != "undefined" {
			c.Response().Header().Set("x-amz-version-id", versionId)
		}
		return c.NoContent(http.StatusOK)
	}

	var owner *S3Owner
	if o.Owner.DisplayName != "" || o.Owner.ID != "" {
		owner = &S3Owner{
			DisplayName: o.Owner.DisplayName,
			ID:          o.Owner.ID,
		}
	}

	var grantList []S3Grant
	for _, g := range o.Grant {
		grantList = append(grantList, S3Grant{
			Grantee:    g.Grantee,
			Permission: g.Permission,
		})
	}

	var restore *S3RestoreInfo
	if o.Restore != nil {
		restore = &S3RestoreInfo{
			OngoingRestore: o.Restore.OngoingRestore,
			ExpiryTime:     o.Restore.ExpiryTime,
		}
	}

	um := map[string]string{}
	for k, v := range o.UserMetadata {
		um[k] = v
	}
	ut := map[string]string{}
	for k, v := range o.UserTags {
		ut[k] = v
	}

	s3Obj := S3ObjectInfo{
		ETag:              o.ETag,
		Key:               o.Key,
		LastModified:      o.LastModified,
		Size:              o.Size,
		ContentType:       o.ContentType,
		Expires:           o.Expires,
		Metadata:          map[string][]string(o.Metadata),
		UserMetadata:      um,
		UserTags:          ut,
		UserTagCount:      o.UserTagCount,
		Owner:             owner,
		Grant:             grantList,
		StorageClass:      o.StorageClass,
		IsLatest:          o.IsLatest,
		IsDeleteMarker:    o.IsDeleteMarker,
		VersionID:         o.VersionID,
		ReplicationStatus: o.ReplicationStatus,
		ReplicationReady:  o.ReplicationReady,
		Expiration:        o.Expiration,
		ExpirationRuleID:  o.ExpirationRuleID,
		NumVersions:       o.NumVersions,
		Restore:           restore,
		ChecksumCRC32:     o.ChecksumCRC32,
		ChecksumCRC32C:    o.ChecksumCRC32C,
		ChecksumSHA1:      o.ChecksumSHA1,
		ChecksumSHA256:    o.ChecksumSHA256,
		ChecksumCRC64NVME: o.ChecksumCRC64NVME,
		ChecksumMode:      o.ChecksumMode,
	}

	return c.JSON(http.StatusOK, s3Obj)
}

func PutS3ObjectFromFile(c echo.Context) error {
	if c.QueryParam("uploadId") != "" && c.QueryParam("partNumber") != "" {
		return uploadPart(c)
	}

	conn, _ := getConnectionName(c)
	bucket := c.Param("BucketName")
	objKey := c.Param("ObjectKey+")

	if c.Request().ContentLength == 0 && !strings.HasSuffix(objKey, "/") {
		userAgent := c.Request().Header.Get("User-Agent")
		if strings.Contains(userAgent, "S3 Browser") {
			objKey = objKey + "/"
			cblog.Infof("S3 Browser folder creation detected, adding trailing slash: %s", objKey)
		}
	}

	body := c.Request().Body
	defer body.Close()

	info, err := cmrt.PutS3ObjectFromReader(conn, bucket, objKey, body, c.Request().ContentLength)
	if err != nil {
		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bucket") {
			errorCode = "NoSuchBucket"
			statusCode = http.StatusNotFound
		}
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucket+"/"+objKey)
	}

	addS3Headers(c)
	c.Response().Header().Set("ETag", info.ETag)
	if info.VersionID != "" {
		c.Response().Header().Set("x-amz-version-id", info.VersionID)
	}
	return c.NoContent(http.StatusOK)
}

func DeleteS3Object(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucket := c.Param("BucketName")
	objKey := c.Param("ObjectKey+")
	versionID := c.QueryParam("versionId")

	cblog.Infof("DeleteS3Object called - bucket: %s, objKey: %s, versionID: %s", bucket, objKey, versionID)

	userAgent := c.Request().Header.Get("User-Agent")
	if strings.Contains(userAgent, "S3 Browser") && !strings.HasSuffix(objKey, "/") {
		objKeyWithSlash := objKey + "/"
		_, err := cmrt.GetS3ObjectInfo(conn, bucket, objKeyWithSlash)
		if err == nil {
			objKey = objKeyWithSlash
			cblog.Infof("S3 Browser folder deletion detected, adding trailing slash: %s", objKey)
		} else {
			cblog.Debugf("No folder found with slash, proceeding with original key: %s", objKey)
		}
	}

	var success bool
	var err error

	// Special handling for DELETE MARKER with null version ID
	if versionID == "null" {
		cblog.Infof("Detected DELETE MARKER with null version ID")

		// For DELETE MARKER with null version ID, we need to use a different approach
		// This typically means deleting the latest version (which is the delete marker)
		success, err = cmrt.DeleteS3ObjectDeleteMarker(conn, bucket, objKey)
		if err != nil {
			cblog.Warnf("Failed to delete DELETE MARKER, trying regular delete: %v", err)
			// Fallback to regular delete
			success, err = cmrt.DeleteS3Object(conn, bucket, objKey)
		}
	} else if versionID != "" && versionID != "undefined" {
		cblog.Infof("Deleting specific version: %s", versionID)
		success, err = cmrt.DeleteS3ObjectVersion(conn, bucket, objKey, versionID)
	} else {
		cblog.Infof("Deleting current version (no valid versionID specified)")
		success, err = cmrt.DeleteS3Object(conn, bucket, objKey)
	}

	if err != nil {
		cblog.Errorf("Failed to delete object/version: %v", err)
		errorCode := "NoSuchKey"
		statusCode := http.StatusNotFound
		if strings.Contains(err.Error(), "bucket") {
			errorCode = "NoSuchBucket"
		} else if strings.Contains(err.Error(), "version") {
			errorCode = "NoSuchVersion"
		}
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucket+"/"+objKey)
	}

	if !success {
		cblog.Errorf("Object/version deletion returned false")
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", "Failed to delete object", "/"+bucket+"/"+objKey)
	}

	cblog.Infof("Successfully deleted object/version - bucket: %s, objKey: %s, versionID: %s", bucket, objKey, versionID)
	addS3Headers(c)
	return c.NoContent(http.StatusNoContent)
}

func DownloadS3Object(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucket := c.Param("BucketName")
	objKey := c.Param("ObjectKey+")
	versionId := c.QueryParam("versionId")

	cblog.Infof("DownloadS3Object - Bucket: %s, Object: %s, VersionId: %s", bucket, objKey, versionId)

	var stream io.ReadCloser
	var err error

	if versionId != "" && versionId != "null" && versionId != "undefined" {
		cblog.Infof("Downloading specific version: %s", versionId)
		stream, err = cmrt.GetS3ObjectStreamWithVersion(conn, bucket, objKey, versionId)
	} else if versionId == "null" {
		cblog.Infof("Downloading null version (original version)")
		stream, err = cmrt.GetS3ObjectStreamWithVersion(conn, bucket, objKey, "null")
	} else {
		cblog.Infof("Downloading latest version")
		stream, err = cmrt.GetS3ObjectStream(conn, bucket, objKey)
	}

	if err != nil {
		cblog.Errorf("Failed to get object stream: %v", err)
		errorCode := "NoSuchKey"
		statusCode := http.StatusNotFound
		if strings.Contains(err.Error(), "bucket") {
			errorCode = "NoSuchBucket"
		} else if strings.Contains(err.Error(), "version") {
			errorCode = "NoSuchVersion"
		}
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucket+"/"+objKey)
	}
	defer stream.Close()

	addS3Headers(c)
	filename := filepath.Base(objKey)
	c.Response().Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Response().Header().Set("Content-Type", "application/octet-stream")

	if versionId != "" && versionId != "null" && versionId != "undefined" {
		c.Response().Header().Set("x-amz-version-id", versionId)
	}

	cblog.Infof("Successfully streaming object: %s", objKey)
	return c.Stream(http.StatusOK, "application/octet-stream", stream)
}

// HandleS3BucketPost handles various POST operations on S3 bucket
func HandleS3BucketPost(c echo.Context) error {
	// 1. multipart upload start
	if c.QueryParam("uploads") != "" || c.QueryParams().Has("uploads") {
		return initiateMultipartUpload(c)
	}

	// 2. multipart upload complete
	if c.QueryParam("uploadId") != "" {
		return completeMultipartUpload(c)
	}

	// 3. delete multiple objects
	if c.QueryParam("delete") != "" ||
		c.QueryParams().Has("delete") ||
		strings.Contains(c.Request().URL.RawQuery, "delete") {
		return deleteMultipleObjects(c)
	}

	// 4. XML-based delete operation
	contentType := c.Request().Header.Get("Content-Type")
	if c.Request().ContentLength > 0 && (contentType == "" || contentType == "application/xml") {
		bodyBytes, err := io.ReadAll(c.Request().Body)
		if err == nil {
			c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			if strings.Contains(string(bodyBytes[:min(len(bodyBytes), 100)]), "<Delete") {
				return deleteMultipleObjects(c)
			}
		}
	}

	// 5. browser-based form upload
	if strings.Contains(contentType, "multipart/form-data") {
		return postObject(c)
	}

	// fallback
	return returnS3Error(c, http.StatusBadRequest, "InvalidRequest", "Unsupported POST request", c.Path())
}

// min helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func initiateMultipartUpload(c echo.Context) error {
	conn, _ := getConnectionName(c)

	bucket := c.Param("BucketName")
	if bucket == "" {
		bucket = c.Param("Name")
	}

	key := c.Param("ObjectKey+")
	if key == "" {
		key = c.Param("*")
	}
	if key == "" {
		key = c.QueryParam("key")
	}

	if key == "" {
		return returnS3Error(
			c,
			http.StatusBadRequest,
			"MissingParameter",
			"key parameter is required",
			"/"+bucket,
		)
	}

	uploadID, err := cmrt.InitiateMultipartUpload(conn, bucket, key)
	if err != nil {
		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			errorCode = "NoSuchBucket"
			statusCode = http.StatusNotFound
		}
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucket+"/"+key)
	}

	type InitiateMultipartUploadResult struct {
		XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
		Xmlns    string   `xml:"xmlns,attr"`
		Bucket   string   `xml:"Bucket"`
		Key      string   `xml:"Key"`
		UploadId string   `xml:"UploadId"`
	}

	resp := InitiateMultipartUploadResult{
		Xmlns:    "http://s3.amazonaws.com/doc/2006-03-01/",
		Bucket:   bucket,
		Key:      key,
		UploadId: uploadID,
	}

	addS3Headers(c)

	xmlData, err := xml.Marshal(resp)
	if err != nil {
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+bucket+"/"+key)
	}

	fullXML := append([]byte(xml.Header), xmlData...)
	return c.Blob(http.StatusOK, "application/xml", fullXML)
}

// completeMultipartUpload completes a multipart upload
func completeMultipartUpload(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucket := c.Param("Name")
	if bucket == "" {
		bucket = c.Param("BucketName")
	}
	key := c.Param("ObjectKey+")
	uploadID := c.QueryParam("uploadId")

	if uploadID == "" {
		return returnS3Error(c, http.StatusBadRequest, "MissingParameter", "uploadId parameter is required", "/"+bucket+"/"+key)
	}

	type Part struct {
		PartNumber int    `xml:"PartNumber"`
		ETag       string `xml:"ETag"`
	}

	type CompleteMultipartUploadRequest struct {
		XMLName xml.Name `xml:"CompleteMultipartUpload"`
		Parts   []Part   `xml:"Part"`
	}

	var req CompleteMultipartUploadRequest
	if err := xml.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return returnS3Error(c, http.StatusBadRequest, "MalformedXML", err.Error(), "/"+bucket+"/"+key)
	}

	var parts []cmrt.CompletePart
	for _, p := range req.Parts {
		parts = append(parts, cmrt.CompletePart{
			PartNumber: p.PartNumber,
			ETag:       p.ETag,
		})
	}

	location, etag, err := cmrt.CompleteMultipartUpload(conn, bucket, key, uploadID, parts)
	if err != nil {
		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			errorCode = "NoSuchUpload"
			statusCode = http.StatusNotFound
		}
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucket+"/"+key)
	}

	type CompleteMultipartUploadResult struct {
		XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
		Xmlns    string   `xml:"xmlns,attr"`
		Location string   `xml:"Location"`
		Bucket   string   `xml:"Bucket"`
		Key      string   `xml:"Key"`
		ETag     string   `xml:"ETag"`
	}

	resp := CompleteMultipartUploadResult{
		Xmlns:    "http://s3.amazonaws.com/doc/2006-03-01/",
		Location: location,
		Bucket:   bucket,
		Key:      key,
		ETag:     etag,
	}

	addS3Headers(c)

	xmlData, err := xml.Marshal(resp)
	if err != nil {
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+bucket+"/"+key)
	}

	fullXML := append([]byte(xml.Header), xmlData...)
	return c.Blob(http.StatusOK, "application/xml", fullXML)
}

// deleteMultipleObjects deletes multiple objects from S3
func deleteMultipleObjects(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucket := c.Param("Name")
	if bucket == "" {
		bucket = c.Param("BucketName")
	}

	cblog.Infof("DeleteMultipleObjects called - bucket: %s", bucket)

	type ObjectToDelete struct {
		Key       string `xml:"Key"`
		VersionId string `xml:"VersionId,omitempty"`
	}

	type Delete struct {
		XMLName xml.Name         `xml:"Delete"`
		Objects []ObjectToDelete `xml:"Object"`
		Quiet   bool             `xml:"Quiet"`
	}

	var req Delete
	if err := xml.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		cblog.Errorf("Failed to decode delete request: %v", err)
		return returnS3Error(c, http.StatusBadRequest, "MalformedXML", err.Error(), "/"+bucket)
	}

	cblog.Infof("Deleting %d objects from bucket %s", len(req.Objects), bucket)

	// Separate objects with and without version IDs
	var keysWithVersions []string
	var keysWithoutVersions []string
	var objectsWithVersions []ObjectToDelete

	for _, obj := range req.Objects {
		if obj.Key != "" {
			cblog.Debugf("Object to delete: %s (VersionId: %s)", obj.Key, obj.VersionId)

			if obj.VersionId != "" && obj.VersionId != "null" {
				// Has version ID
				objectsWithVersions = append(objectsWithVersions, obj)
				keysWithVersions = append(keysWithVersions, obj.Key)
			} else {
				// No version ID (legacy object or current version)
				keysWithoutVersions = append(keysWithoutVersions, obj.Key)
			}
		} else {
			cblog.Warnf("Skipping empty key in delete request")
		}
	}

	cblog.Infof("Objects with versions: %d, Objects without versions: %d",
		len(keysWithVersions), len(keysWithoutVersions))

	var allResults []cmrt.DeleteResult

	// Delete objects without version IDs (regular delete)
	if len(keysWithoutVersions) > 0 {
		cblog.Infof("Deleting %d objects without version IDs", len(keysWithoutVersions))

		results, err := cmrt.DeleteMultipleObjects(conn, bucket, keysWithoutVersions)
		if err != nil {
			// If bulk delete not supported, try individual deletes
			if strings.Contains(err.Error(), "not implemented") || strings.Contains(err.Error(), "NotImplemented") {
				cblog.Warnf("Bulk delete not supported, falling back to individual deletes for objects without versions")

				for _, key := range keysWithoutVersions {
					_, deleteErr := cmrt.DeleteS3Object(conn, bucket, key)
					if deleteErr != nil {
						allResults = append(allResults, cmrt.DeleteResult{
							Key:     key,
							Success: false,
							Error:   deleteErr.Error(),
						})
					} else {
						allResults = append(allResults, cmrt.DeleteResult{
							Key:     key,
							Success: true,
						})
					}
				}
			} else {
				cblog.Errorf("Failed to delete multiple objects without versions: %v", err)
				return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+bucket)
			}
		} else {
			allResults = append(allResults, results...)
		}
	}

	// For objects with version IDs, we need to use individual delete calls
	// because CB-Spider's DeleteMultipleObjects doesn't support version IDs
	if len(objectsWithVersions) > 0 {
		cblog.Infof("Deleting %d objects with version IDs using individual calls", len(objectsWithVersions))

		for _, obj := range objectsWithVersions {
			// For versioned objects, we need to call a different function
			// Since CB-Spider doesn't have a direct function for versioned deletes,
			// we'll try to delete using the key and hope the S3 provider handles it
			_, deleteErr := cmrt.DeleteS3Object(conn, bucket, obj.Key)
			if deleteErr != nil {
				cblog.Errorf("Failed to delete versioned object %s (version %s): %v", obj.Key, obj.VersionId, deleteErr)
				allResults = append(allResults, cmrt.DeleteResult{
					Key:     obj.Key,
					Success: false,
					Error:   deleteErr.Error(),
				})
			} else {
				cblog.Infof("Successfully deleted versioned object %s (version %s)", obj.Key, obj.VersionId)
				allResults = append(allResults, cmrt.DeleteResult{
					Key:     obj.Key,
					Success: true,
				})
			}
		}
	}

	// Build response
	type Deleted struct {
		Key string `xml:"Key"`
	}

	type Error struct {
		Key     string `xml:"Key"`
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}

	type DeleteResult struct {
		XMLName xml.Name  `xml:"DeleteResult"`
		Xmlns   string    `xml:"xmlns,attr"`
		Deleted []Deleted `xml:"Deleted"`
		Error   []Error   `xml:"Error"`
	}

	resp := DeleteResult{
		Xmlns: "http://s3.amazonaws.com/doc/2006-03-01/",
	}

	for _, result := range allResults {
		if result.Success {
			resp.Deleted = append(resp.Deleted, Deleted{Key: result.Key})
		} else {
			// Map common error messages to S3 error codes
			errorCode := "InternalError"
			errorMsg := result.Error

			if strings.Contains(result.Error, "not found") ||
				strings.Contains(result.Error, "NoSuchKey") {
				errorCode = "NoSuchKey"
			} else if strings.Contains(result.Error, "access denied") ||
				strings.Contains(result.Error, "AccessDenied") {
				errorCode = "AccessDenied"
			} else if strings.Contains(result.Error, "not implemented") {
				errorCode = "NotImplemented"
			}

			resp.Error = append(resp.Error, Error{
				Key:     result.Key,
				Code:    errorCode,
				Message: errorMsg,
			})
		}
	}

	addS3Headers(c)

	xmlData, err := xml.Marshal(resp)
	if err != nil {
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+bucket)
	}

	fullXML := append([]byte(xml.Header), xmlData...)
	cblog.Debugf("Returning delete result with %d deleted and %d errors", len(resp.Deleted), len(resp.Error))
	return c.Blob(http.StatusOK, "application/xml", fullXML)
}

// postObject handles browser-based file upload using HTML form
func postObject(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucket := c.Param("Name")
	if bucket == "" {
		bucket = c.Param("BucketName")
	}

	form, err := c.MultipartForm()
	if err != nil {
		return returnS3Error(c, http.StatusBadRequest, "MalformedPOSTRequest", err.Error(), "/"+bucket)
	}

	key := form.Value["key"][0]
	if key == "" {
		return returnS3Error(c, http.StatusBadRequest, "MissingFields", "key is required", "/"+bucket)
	}

	files := form.File["file"]
	if len(files) == 0 {
		return returnS3Error(c, http.StatusBadRequest, "MissingFields", "file is required", "/"+bucket)
	}

	file, err := files[0].Open()
	if err != nil {
		return returnS3Error(c, http.StatusInternalServerError, "InternalError", err.Error(), "/"+bucket+"/"+key)
	}
	defer file.Close()

	_, err = cmrt.PutS3ObjectFromReader(conn, bucket, key, file, files[0].Size)
	if err != nil {
		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bucket") {
			errorCode = "NoSuchBucket"
			statusCode = http.StatusNotFound
		}
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucket+"/"+key)
	}

	successRedirect := form.Value["success_action_redirect"]
	if len(successRedirect) > 0 && successRedirect[0] != "" {
		return c.Redirect(http.StatusSeeOther, successRedirect[0])
	}

	addS3Headers(c)
	return c.NoContent(http.StatusNoContent)
}

// uploadPart uploads a part in a multipart upload
func uploadPart(c echo.Context) error {
	conn, _ := getConnectionName(c)
	bucket := c.Param("BucketName")
	key := c.Param("ObjectKey+")
	uploadID := c.QueryParam("uploadId")
	partNumberStr := c.QueryParam("partNumber")

	if uploadID == "" || partNumberStr == "" {
		return returnS3Error(c, http.StatusBadRequest, "MissingParameter", "uploadId and partNumber are required", "/"+bucket+"/"+key)
	}

	partNumber, err := strconv.Atoi(partNumberStr)
	if err != nil {
		return returnS3Error(c, http.StatusBadRequest, "InvalidArgument", "invalid partNumber", "/"+bucket+"/"+key)
	}

	body := c.Request().Body
	defer body.Close()

	etag, err := cmrt.UploadPart(conn, bucket, key, uploadID, partNumber, body, c.Request().ContentLength)
	if err != nil {
		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			errorCode = "NoSuchUpload"
			statusCode = http.StatusNotFound
		}
		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+bucket+"/"+key)
	}

	addS3Headers(c)
	c.Response().Header().Set("ETag", etag)
	return c.NoContent(http.StatusOK)
}

// ForceEmptyS3Bucket forcefully empties a bucket but keeps the bucket
func ForceEmptyS3Bucket(c echo.Context) error {
	conn, _ := getConnectionName(c)
	name := c.Param("Name")

	cblog.Infof("ForceEmptyS3Bucket called - Bucket: %s, Connection: %s", name, conn)
	cblog.Infof("Request method: %s, URL: %s", c.Request().Method, c.Request().URL.String())
	cblog.Infof("Query parameters: %v", c.QueryParams())

	// Check for force empty parameter
	if c.QueryParam("empty") == "" && c.Request().Header.Get("X-Force-Empty") == "" {
		return returnS3Error(c, http.StatusBadRequest, "InvalidRequest",
			"Force empty requires 'empty' query parameter or X-Force-Empty header", "/"+name)
	}

	cblog.Infof("Force empty confirmed for bucket %s", name)

	success, err := cmrt.ForceEmptyBucket(conn, name)
	if err != nil {
		cblog.Errorf("Failed to force empty bucket %s: %v", name, err)

		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError

		if strings.Contains(err.Error(), "not found") {
			errorCode = "NoSuchBucket"
			statusCode = http.StatusNotFound
		} else if strings.Contains(err.Error(), "access denied") {
			errorCode = "AccessDenied"
			statusCode = http.StatusForbidden
		}

		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+name)
	}

	if !success {
		cblog.Errorf("Force empty returned false for bucket %s", name)
		return returnS3Error(c, http.StatusInternalServerError, "InternalError",
			"Force empty failed for unknown reason", "/"+name)
	}

	cblog.Infof("Successfully force emptied bucket %s", name)
	addS3Headers(c)
	return c.NoContent(http.StatusNoContent)
}

// ForceDeleteS3Bucket forcefully empties and deletes a bucket
func ForceDeleteS3Bucket(c echo.Context) error {
	conn, _ := getConnectionName(c)
	name := c.Param("Name")

	cblog.Infof("ForceDeleteS3Bucket called - Bucket: %s, Connection: %s", name, conn)
	cblog.Infof("Request method: %s, URL: %s", c.Request().Method, c.Request().URL.String())
	cblog.Infof("Query parameters: %v", c.QueryParams())

	// Check for force delete parameter
	if c.QueryParam("force") == "" && c.Request().Header.Get("X-Force-Delete") == "" {
		return returnS3Error(c, http.StatusBadRequest, "InvalidRequest",
			"Force delete requires 'force' query parameter or X-Force-Delete header", "/"+name)
	}

	cblog.Infof("Force delete confirmed for bucket %s", name)

	success, err := cmrt.ForceEmptyAndDeleteBucket(conn, name)
	if err != nil {
		cblog.Errorf("Failed to force delete bucket %s: %v", name, err)

		errorCode := "InternalError"
		statusCode := http.StatusInternalServerError

		if strings.Contains(err.Error(), "not found") {
			errorCode = "NoSuchBucket"
			statusCode = http.StatusNotFound
		} else if strings.Contains(err.Error(), "not empty") {
			errorCode = "BucketNotEmpty"
			statusCode = http.StatusConflict
		} else if strings.Contains(err.Error(), "access denied") {
			errorCode = "AccessDenied"
			statusCode = http.StatusForbidden
		}

		return returnS3Error(c, statusCode, errorCode, err.Error(), "/"+name)
	}

	if !success {
		cblog.Errorf("Force delete returned false for bucket %s", name)
		return returnS3Error(c, http.StatusInternalServerError, "InternalError",
			"Force delete failed for unknown reason", "/"+name)
	}

	cblog.Infof("Successfully force deleted bucket %s", name)
	addS3Headers(c)
	return c.NoContent(http.StatusNoContent)
}
