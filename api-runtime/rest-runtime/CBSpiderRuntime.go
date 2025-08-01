// Rest Runtime Server of CB-Spider.
// The CB-Spider is a sub-Framework of the Cloud-Barista Multi-Cloud Project.
// The CB-Spider Mission is to connect all the clouds with a single interface.
//
//      * Cloud-Barista: https://github.com/cloud-barista
//
// by CB-Spider Team, 2019.10.

package restruntime

import (
	"bytes"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"path/filepath"
	"strings"
	"time"

	"net/http"
	"os"

	cblogger "github.com/cloud-barista/cb-log"
	cr "github.com/cloud-barista/cb-spider/api-runtime/common-runtime"
	aw "github.com/cloud-barista/cb-spider/api-runtime/rest-runtime/admin-web"
	infostore "github.com/cloud-barista/cb-spider/info-store"

	"github.com/sirupsen/logrus"

	// REST API (echo)
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	lblog "github.com/labstack/gommon/log"

	// echo-swagger middleware
	_ "github.com/cloud-barista/cb-spider/api"
	echoSwagger "github.com/swaggo/echo-swagger"

	"github.com/natefinch/lumberjack"
)

var cblog *logrus.Logger

// @title CB-Spider REST API
// @version latest
// @description **🕷️ [User Guide](https://github.com/cloud-barista/cb-spider/wiki/features-and-usages)**  **🕷️ [API Guide](https://github.com/cloud-barista/cb-spider/wiki/REST-API-Examples)**

// @contact.name API Support
// @contact.url http://cloud-barista.github.io
// @contact.email contact-to-cloud-barista@googlegroups.com

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:1024

// @BasePath /spider

// @schemes http

// @securityDefinitions.basic BasicAuth

func init() {
	cblog = cblogger.GetLogger("CLOUD-BARISTA")
	currentTime := time.Now()
	cr.StartTime = currentTime.Format("2006.01.02 15:04:05 Mon")
	cr.MiddleStartTime = currentTime.Format("2006.01.02.15:04:05")
	cr.ShortStartTime = fmt.Sprintf("T%02d:%02d:%02d", currentTime.Hour(), currentTime.Minute(), currentTime.Second())

	// REST and GO SERVER_ADDRESS since v0.4.4
	cr.ServerIPorName = getServerIPorName("SERVER_ADDRESS")
	cr.ServerPort = getServerPort("SERVER_ADDRESS")

	// REST SERVICE_ADDRESS for AdminWeb since v0.4.4
	cr.ServiceIPorName = getServiceIPorName("SERVICE_ADDRESS")
	cr.ServicePort = getServicePort("SERVICE_ADDRESS")
}

// ex) {"POST", "/driver", registerCloudDriver}
type route struct {
	method, path string
	function     echo.HandlerFunc
}

// JSON Simple message struct
type SimpleMsg struct {
	Message string `json:"message" validate:"required" example:"Any message" description:"A simple message to be returned by the API"`
}

//// CB-Spider Servcie Address Configuration
////   cf)  https://github.com/cloud-barista/cb-spider/wiki/CB-Spider-Service-Address-Configuration

// REST and GO SERVER_ADDRESS since v0.4.4

// unset                           # default: like 'curl ifconfig.co':1024
// SERVER_ADDRESS="1.2.3.4:3000"  # => 1.2.3.4:3000
// SERVER_ADDRESS=":3000"         # => like 'curl ifconfig.co':3000
// SERVER_ADDRESS="localhost"      # => localhost:1024
// SERVER_ADDRESS="1.2.3.4:3000"        # => 1.2.3.4::3000
func getServerIPorName(env string) string {

	hostEnv := os.Getenv(env) // SERVER_ADDRESS or SERVICE_ADDRESS

	if hostEnv == "" {
		return "localhost"
	}

	// "1.2.3.4" or "localhost"
	if !strings.Contains(hostEnv, ":") {
		return hostEnv
	}

	strs := strings.Split(hostEnv, ":")
	if strs[0] == "" { // ":31024"
		return "localhost"
	} else { // "1.2.3.4:31024" or "localhost:31024"
		return strs[0]
	}
}

func getServerPort(env string) string {
	// default REST Service Port
	servicePort := ":1024"

	hostEnv := os.Getenv(env) // SERVER_ADDRESS or SERVICE_ADDRESS
	if hostEnv == "" {
		return servicePort
	}

	// "1.2.3.4" or "localhost"
	if !strings.Contains(hostEnv, ":") {
		return servicePort
	}

	// ":31024" or "1.2.3.4:31024" or "localhost:31024"
	strs := strings.Split(hostEnv, ":")
	servicePort = ":" + strs[1]

	return servicePort
}

// unset  SERVER_ADDRESS => SERVICE_ADDRESS
func getServiceIPorName(env string) string {
	hostEnv := os.Getenv(env)
	if hostEnv == "" {
		return cr.ServerIPorName
	}
	return getServerIPorName(env)
}

// unset  SERVER_ADDRESS => SERVICE_ADDRESS
func getServicePort(env string) string {
	hostEnv := os.Getenv(env)
	if hostEnv == "" {
		return cr.ServerPort
	}
	return getServerPort(env)
}

func RunServer() {

	//======================================= setup routes
	routes := []route{
		//----------root
		{"GET", "", aw.SpiderInfo},
		{"GET", "/", aw.SpiderInfo},

		//----------Swagger
		{"GET", "/api", echoSwagger.EchoWrapHandler(echoSwagger.DocExpansion("none"))},
		{"GET", "/api/", echoSwagger.EchoWrapHandler(echoSwagger.DocExpansion("none"))},
		{"GET", "/api/*", echoSwagger.EchoWrapHandler(echoSwagger.DocExpansion("none"))},

		//----------EndpointInfo
		{"GET", "/endpointinfo", endpointInfo},

		//---------- Server VersionInfo
		{"GET", "/version", versionInfo},

		//----------healthcheck
		{"GET", "/healthcheck", healthCheck},
		{"GET", "/health", healthCheck},
		{"GET", "/ping", healthCheck},
		{"GET", "/readyz", healthCheck},

		//----------SystemStatsInfo Handler
		{"GET", "/sysstats/system", FetchSystemInfo},
		{"GET", "/sysstats/usage", FetchResourceUsage},

		//----------CloudOS
		{"GET", "/cloudos", ListCloudOS},

		//----------CloudOSMetaInfo
		{"GET", "/cloudos/metainfo/:CloudOSName", GetCloudOSMetaInfo},

		//----------CloudDriver CapabilityInfo
		{"GET", "/driver/capability", GetDriverCapability},

		//----------CloudDriverInfo
		{"POST", "/driver", RegisterCloudDriver},
		{"POST", "/driver/upload", UploadCloudDriver},
		{"GET", "/driver", ListCloudDriver},
		{"GET", "/driver/:DriverName", GetCloudDriver},
		{"DELETE", "/driver/:DriverName", UnRegisterCloudDriver},

		//----------CredentialInfo
		{"POST", "/credential", RegisterCredential},
		{"GET", "/credential", ListCredential},
		{"GET", "/credential/:CredentialName", GetCredential},
		{"DELETE", "/credential/:CredentialName", UnRegisterCredential},

		//----------RegionInfo
		{"POST", "/region", RegisterRegion},
		{"GET", "/region", ListRegion},
		{"GET", "/region/:RegionName", GetRegion},
		{"DELETE", "/region/:RegionName", UnRegisterRegion},

		//----------ConnectionConfigInfo
		{"POST", "/connectionconfig", CreateConnectionConfig},
		{"GET", "/connectionconfig", ListConnectionConfig},
		{"GET", "/connectionconfig/:ConfigName", GetConnectionConfig},
		{"DELETE", "/connectionconfig/:ConfigName", DeleteConnectionConfig},
		//-- for dashboard
		{"GET", "/countconnectionconfig", CountAllConnections},
		{"GET", "/countconnectionconfig/:ProviderName", CountConnectionsByProvider},

		//-------------------------------------------------------------------//

		//----------RegionZone Handler
		{"GET", "/regionzone", ListRegionZone},
		{"GET", "/regionzone/:Name", GetRegionZone},
		{"GET", "/orgregion", ListOrgRegion},
		{"GET", "/orgzone", ListOrgZone},
		// by driverName & credentialName
		{"GET", "/preconfig/regionzone", ListRegionZonePreConfig},
		{"GET", "/preconfig/regionzone/:Name", GetRegionZonePreConfig},
		{"GET", "/preconfig/orgregion", ListOrgRegionPreConfig},

		//----------PriceInfo Handler
		{"GET", "/productfamily/:RegionName", ListProductFamily},
		{"GET", "/priceinfo/vm/:RegionName", GetVMPriceInfo},  // GET with a body for backward compatibility
		{"POST", "/priceinfo/vm/:RegionName", GetVMPriceInfo}, // POST with a body for standard

		//----------Image Handler
		{"GET", "/vmimage", ListImage},
		{"GET", "/vmimage/:Name", GetImage},

		//----------VMSpec Handler
		{"GET", "/vmspec", ListVMSpec},
		{"GET", "/vmspec/:Name", GetVMSpec},
		{"GET", "/vmorgspec", ListOrgVMSpec},
		{"GET", "/vmorgspec/:Name", GetOrgVMSpec},

		//----------VPC Handler
		{"POST", "/regvpc", RegisterVPC},
		{"DELETE", "/regvpc/:Name", UnregisterVPC},
		{"POST", "/regsubnet", RegisterSubnet},
		{"DELETE", "/regsubnet/:Name", UnregisterSubnet},

		{"POST", "/vpc", CreateVPC},
		{"GET", "/vpc", ListVPC},
		{"GET", "/vpc/:Name", GetVPC},
		{"DELETE", "/vpc/:Name", DeleteVPC},
		//-- for subnet
		{"POST", "/vpc/:VPCName/subnet", AddSubnet},
		{"GET", "/vpc/:VPCName/subnet/:Name", GetSubnet},
		{"DELETE", "/vpc/:VPCName/subnet/:SubnetName", RemoveSubnet},
		{"DELETE", "/vpc/:VPCName/cspsubnet/:Id", RemoveCSPSubnet},
		//-- for management
		{"GET", "/allvpc", ListAllVPC},
		{"GET", "/allvpcinfo", ListAllVPCInfo},
		{"DELETE", "/cspvpc/:Id", DeleteCSPVPC},
		//-- for dashboard
		{"GET", "/countvpc", CountAllVPCs},
		{"GET", "/countvpc/:ConnectionName", CountVPCsByConnection},
		{"GET", "/countsubnet", CountAllSubnets},
		{"GET", "/countsubnet/:ConnectionName", CountSubnetsByConnection},

		//----------SecurityGroup Handler
		{"GET", "/getsecuritygroupowner", GetSGOwnerVPC},
		{"POST", "/getsecuritygroupowner", GetSGOwnerVPC},
		{"POST", "/regsecuritygroup", RegisterSecurity},
		{"DELETE", "/regsecuritygroup/:Name", UnregisterSecurity},

		{"POST", "/securitygroup", CreateSecurity},
		{"GET", "/securitygroup", ListSecurity},
		{"GET", "/securitygroup/:Name", GetSecurity},
		{"GET", "/securitygroup/vpc/:VPCName", ListVpcSecurity},
		{"DELETE", "/securitygroup/:Name", DeleteSecurity},
		//-- for rule
		{"POST", "/securitygroup/:SGName/rules", AddRules},
		{"DELETE", "/securitygroup/:SGName/rules", RemoveRules}, // no force option
		// no CSP Option, {"DELETE", "/securitygroup/:SGName/csprules", RemoveCSPRules},
		//-- for management
		{"GET", "/allsecuritygroup", ListAllSecurity},
		{"GET", "/allsecuritygroupinfo", ListAllSecurityGroupInfo},
		{"DELETE", "/cspsecuritygroup/:Id", DeleteCSPSecurity},
		//-- for dashboard
		{"GET", "/countsecuritygroup", CountAllSecurityGroups},
		{"GET", "/countsecuritygroup/:ConnectionName", CountSecurityGroupsByConnection},

		//----------KeyPair Handler
		{"POST", "/regkeypair", RegisterKey},
		{"DELETE", "/regkeypair/:Name", UnregisterKey},

		{"POST", "/keypair", CreateKey},
		{"GET", "/keypair", ListKey},
		{"GET", "/keypair/:Name", GetKey},
		{"DELETE", "/keypair/:Name", DeleteKey},
		//-- for management
		{"GET", "/allkeypair", ListAllKey},
		{"GET", "/allkeypairinfo", ListAllKeyPairInfo},
		{"DELETE", "/cspkeypair/:Id", DeleteCSPKey},
		//-- for dashboard
		{"GET", "/countkeypair", CountAllKeys},
		{"GET", "/countkeypair/:ConnectionName", CountKeysByConnection},
		/*
			//----------VNic Handler
			{"POST", "/vnic", createVNic},
			{"GET", "/vnic", listVNic},
			{"GET", "/vnic/:VNicId", getVNic},
			{"DELETE", "/vnic/:VNicId", deleteVNic},

			//----------PublicIP Handler
			{"POST", "/publicip", createPublicIP},
			{"GET", "/publicip", listPublicIP},
			{"GET", "/publicip/:PublicIPId", getPublicIP},
			{"DELETE", "/publicip/:PublicIPId", deletePublicIP},
		*/
		//----------VM Handler
		{"GET", "/getvmusingresources", GetVMUsingRS},
		{"POST", "/getvmusingresources", GetVMUsingRS},
		{"POST", "/regvm", RegisterVM},
		{"DELETE", "/regvm/:Name", UnregisterVM},

		{"POST", "/vm", StartVM},
		{"GET", "/vm", ListVM},
		{"GET", "/vm/:Name", GetVM},
		{"DELETE", "/vm/:Name", TerminateVM},

		{"GET", "/vmstatus", ListVMStatus},
		{"GET", "/vmstatus/:Name", GetVMStatus},

		{"GET", "/controlvm/:Name", ControlVM}, // suspend, resume, reboot
		// only for AdminWeb
		{"PUT", "/controlvm/:Name", ControlVM}, // suspend, resume, reboot

		//-- for management
		{"GET", "/allvm", ListAllVM},
		{"GET", "/allvminfo", ListAllVMInfo},
		{"DELETE", "/cspvm/:Id", TerminateCSPVM},
		//-- for dashboard
		{"GET", "/countvm", CountAllVMs},
		{"GET", "/countvm/:ConnectionName", CountVMsByConnection},

		//----------NLB Handler
		{"GET", "/getnlbowner", GetNLBOwnerVPC},
		{"POST", "/getnlbowner", GetNLBOwnerVPC},
		{"POST", "/regnlb", RegisterNLB},
		{"DELETE", "/regnlb/:Name", UnregisterNLB},

		{"POST", "/nlb", CreateNLB},
		{"GET", "/nlb", ListNLB},
		{"GET", "/nlb/:Name", GetNLB},
		{"DELETE", "/nlb/:Name", DeleteNLB},
		//-- for vm
		{"POST", "/nlb/:Name/vms", AddNLBVMs},
		{"DELETE", "/nlb/:Name/vms", RemoveNLBVMs}, // no force option
		{"PUT", "/nlb/:Name/listener", ChangeListener},
		{"PUT", "/nlb/:Name/vmgroup", ChangeVMGroup},
		{"PUT", "/nlb/:Name/healthchecker", ChangeHealthChecker},
		{"GET", "/nlb/:Name/health", GetVMGroupHealthInfo},

		//-- for management
		{"GET", "/allnlb", ListAllNLB},
		{"GET", "/allnlbinfo", ListAllNLBInfo},
		{"DELETE", "/cspnlb/:Id", DeleteCSPNLB},
		//-- for dashboard
		{"GET", "/countnlb", CountAllNLBs},
		{"GET", "/countnlb/:ConnectionName", CountNLBsByConnection},

		//----------Disk Handler
		{"POST", "/regdisk", RegisterDisk},
		{"DELETE", "/regdisk/:Name", UnregisterDisk},

		{"POST", "/disk", CreateDisk},
		{"GET", "/disk", ListDisk},
		{"GET", "/disk/:Name", GetDisk},
		{"PUT", "/disk/:Name/size", IncreaseDiskSize},
		{"DELETE", "/disk/:Name", DeleteDisk},
		//-- for vm
		{"PUT", "/disk/:Name/attach", AttachDisk},
		{"PUT", "/disk/:Name/detach", DetachDisk},

		//-- for management
		{"GET", "/alldisk", ListAllDisk},
		{"GET", "/alldiskinfo", ListAllDiskInfo},
		{"DELETE", "/cspdisk/:Id", DeleteCSPDisk},
		//-- for dashboard
		{"GET", "/countdisk", CountAllDisks},
		{"GET", "/countdisk/:ConnectionName", CountDisksByConnection},

		//----------MyImage Handler
		{"POST", "/regmyimage", RegisterMyImage},
		{"DELETE", "/regmyimage/:Name", UnregisterMyImage},

		{"POST", "/myimage", SnapshotVM},
		{"GET", "/myimage", ListMyImage},
		{"GET", "/myimage/:Name", GetMyImage},
		{"DELETE", "/myimage/:Name", DeleteMyImage},

		//-- for management
		{"GET", "/allmyimage", ListAllMyImage},
		{"GET", "/allmyimageinfo", ListAllMyImageInfo},
		{"DELETE", "/cspmyimage/:Id", DeleteCSPMyImage},
		//-- for dashboard
		{"GET", "/countmyimage", CountAllMyImages},
		{"GET", "/countmyimage/:ConnectionName", CountMyImagesByConnection},

		//----------Cluster Handler
		{"GET", "/getclusterowner", GetClusterOwnerVPC},
		{"POST", "/getclusterowner", GetClusterOwnerVPC},
		{"POST", "/regcluster", RegisterCluster},
		{"DELETE", "/regcluster/:Name", UnregisterCluster},

		{"POST", "/cluster", CreateCluster},
		{"GET", "/cluster", ListCluster},
		{"GET", "/cluster/:Name", GetCluster},
		{"DELETE", "/cluster/:Name", DeleteCluster},
		//-- for NodeGroup
		{"POST", "/cluster/:Name/nodegroup", AddNodeGroup},
		{"DELETE", "/cluster/:Name/nodegroup/:NodeGroupName", RemoveNodeGroup},
		{"PUT", "/cluster/:Name/nodegroup/:NodeGroupName/onautoscaling", SetNodeGroupAutoScaling},
		{"PUT", "/cluster/:Name/nodegroup/:NodeGroupName/autoscalesize", ChangeNodeGroupScaling},
		{"PUT", "/cluster/:Name/upgrade", UpgradeCluster},
		{"GET", "/cspvm/:Id", GetCSPVM},

		//-- for management
		{"GET", "/allcluster", ListAllCluster},
		{"GET", "/allclusterinfo", ListAllClusterInfo},
		{"DELETE", "/cspcluster/:Id", DeleteCSPCluster},
		//-- for dashboard
		{"GET", "/countcluster", CountAllClusters},
		{"GET", "/countcluster/:ConnectionName", CountClustersByConnection},

		//----------Tag Handler
		{"POST", "/tag", AddTag},
		{"GET", "/tag", ListTag},
		{"GET", "/tag/:Key", GetTag},
		{"DELETE", "/tag/:Key", RemoveTag},

		//----------FileSystem Handler
		// {"POST", "/regfilesystem", RegisterFileSystem},
		// {"DELETE", "/regfilesystem/:Name", UnregisterFileSystem},

		{"POST", "/filesystem", CreateFileSystem},
		{"GET", "/filesystem", ListFileSystem},
		{"GET", "/filesystem/:Name", GetFileSystem},
		{"DELETE", "/filesystem/:Name", DeleteFileSystem},
		// -- for AccessSubnet
		{"POST", "/filesystem/:Name/accesssubnet", AddAccessSubnet},
		{"GET", "/filesystem/:Name/accesssubnet", ListAccessSubnet},
		{"DELETE", "/filesystem/:Name/accesssubnet", RemoveAccessSubnet},

		//----------Destory All Resources in a Connection
		{"DELETE", "/destroy", Destroy},

		//----------checking TCP and UDP ports for NLB
		{"GET", "/check/tcp", CheckTCPPort},
		{"GET", "/check/udp", CheckUDPPort},

		//-------------------------------------------------------------------//
		//----------Additional Info
		{"GET", "/cspresourcename/:Name", GetCSPResourceName},
		{"GET", "/cspresourceinfo/:Name", GetCSPResourceInfo},

		//----------AnyCall Handler
		{"POST", "/anycall", AnyCall},

		//----------WebMon Handler
		{"GET", "/adminweb/vmmon", aw.VMMointoring},

		//////////////////////////////////////////////////////////////
		//------------------ Spiderlet Zone ------------------------

		{"POST", "/spiderlet/anycall", SpiderletAnyCall},

		//----------WebMon Handler
		{"GET", "/adminweb/spiderlet/vmmon", aw.SpiderletVMMointoring},

		//------------------ Spiderlet Zone ------------------------
		//////////////////////////////////////////////////////////////

		//-------------------------------------------------------------------//
		//----------SPLock Info
		{"GET", "/splockinfo", GetAllSPLockInfo},
		//----------SSH RUN
		{"POST", "/sshrun", SSHRun},

		//----------AdminWeb Handler
		{"GET", "/adminweb1", aw.Frame},
		{"GET", "/adminweb1/", aw.Frame},
		{"GET", "/adminweb/top", aw.Top},
		{"GET", "/adminweb/log", aw.Log},

		{"GET", "/adminweb", aw.MainPage},
		{"GET", "/adminweb/", aw.MainPage},
		{"GET", "/adminweb/left_menu", aw.LeftMenu},
		{"GET", "/adminweb/body_frame", aw.BodyFrame},

		{"GET", "/adminweb/dashboard", aw.Dashboard},

		{"GET", "/adminweb/driver1", aw.Driver},
		{"GET", "/adminweb/driver", aw.DriverManagement},

		{"GET", "/adminweb/credential1", aw.Credential},
		{"GET", "/adminweb/credential", aw.CredentialManagement},

		{"GET", "/adminweb/region1", aw.Region},
		{"GET", "/adminweb/region", aw.RegionManagement},

		{"GET", "/adminweb/connectionconfig1", aw.Connectionconfig},
		{"GET", "/adminweb/connectionconfig", aw.ConnectionManagement},

		{"GET", "/adminweb/dashboard", aw.Dashboard},

		{"GET", "/adminweb/spiderinfo", aw.SpiderInfo},

		{"GET", "/adminweb/sysstats", aw.SystemStatsInfoPage},

		{"GET", "/adminweb/vpc/:ConnectConfig", aw.VPCSubnetManagement},
		{"GET", "/adminweb/vpcmgmt/:ConnectConfig", aw.VPCMgmt},
		{"GET", "/adminweb/securitygroup/:ConnectConfig", aw.SecurityGroupManagement},
		{"GET", "/adminweb/securitygroupmgmt/:ConnectConfig", aw.SecurityGroupMgmt},
		{"GET", "/adminweb/keypair/:ConnectConfig", aw.KeyPairManagement},
		{"GET", "/adminweb/keypairmgmt/:ConnectConfig", aw.KeyPairMgmt},
		{"GET", "/adminweb/vm/:ConnectConfig", aw.VMManagement},
		{"GET", "/adminweb/vmmgmt/:ConnectConfig", aw.VMMgmt},
		{"GET", "/adminweb/nlb/:ConnectConfig", aw.NLBManagement},
		{"GET", "/adminweb/nlbmgmt/:ConnectConfig", aw.NLBMgmt},
		{"GET", "/adminweb/disk/:ConnectConfig", aw.DiskManagement},
		{"GET", "/adminweb/diskmgmt/:ConnectConfig", aw.DiskMgmt},
		{"GET", "/adminweb/cluster/:ConnectConfig", aw.ClusterManagement},
		{"GET", "/adminweb/clustermgmt/:ConnectConfig", aw.ClusterMgmt},
		{"GET", "/adminweb/myimage/:ConnectConfig", aw.MyImageManagement},
		{"GET", "/adminweb/myimagemgmt/:ConnectConfig", aw.MyImageMgmt},
		{"GET", "/adminweb/vmimage/:ConnectConfig", aw.VMImage},
		{"GET", "/adminweb/vmspec/:ConnectConfig", aw.VMSpec},
		{"GET", "/adminweb/regionzone/:ConnectConfig", aw.RegionZone},
		{"GET", "/adminweb/priceinfo/:ConnectConfig", aw.PriceInfoRequest},
		{"GET", "/adminweb/priceinfotablelist/:ProductFamily/:RegionName/:ConnectConfig", aw.PriceInfoTableList},
		// download price info with JSON file
		{"GET", "/adminweb/priceinfo/download/:FileName", aw.DownloadPriceInfo},

		{"GET", "/adminweb/s3/:ConnectConfig", aw.S3Management},

		{"GET", "/adminweb/cmd-agent", aw.CmdAgent},
		{"POST", "/adminweb/generate-cmd", aw.GenerateCmd},

		{"GET", "/adminweb/calllog-analyzer", aw.CallLogAnalyzer},
		{"POST", "/adminweb/analyze-logs", aw.AnalyzeLogs},
		{"GET", "/adminweb/read-logs", aw.GetReadLogs},

		//----------SSH WebTerminal Handler
		{"GET", "/adminweb/sshwebterminal/ws", aw.HandleWebSocket},
	}

	// for Standard S3 API - Order matters! More specific routes should come first
	s3Routes := []route{
		{"GET", "/", ListS3Buckets},

		// Bucket-level operations (with query parameters)
		{"GET", "/:Name", GetS3Bucket}, // Handles ?versioning, ?cors, ?policy, ?location, ?versions, and list objects
		{"GET", "/:Name/", GetS3Bucket},
		{"HEAD", "/:Name", GetS3Bucket},
		{"PUT", "/:Name", CreateS3Bucket}, // Handles bucket creation AND bucket config (redirects to GetS3Bucket)
		{"DELETE", "/:Name", DeleteS3Bucket},

		//--------- don't change the order of these routes
		{"POST", "/:BucketName/:ObjectKey+", HandleS3BucketPost},
		{"POST", "/:Name", HandleS3BucketPost},
		{"POST", "/:Name/", HandleS3BucketPost},
		//--------- don't change the order of these routes

		// Object-level operations
		{"PUT", "/:BucketName/:ObjectKey+", PutS3ObjectFromFile},
		{"HEAD", "/:BucketName/:ObjectKey+", GetS3ObjectInfo},
		{"GET", "/:BucketName/:ObjectKey+", DownloadS3Object},
		{"DELETE", "/:BucketName/:ObjectKey+", DeleteS3Object},
	}

	//======================================= setup routes

	// Run API Server
	ApiServer(routes, s3Routes)

}

func RunTLSServer(certFile, keyFile, caCertFile string, port int) {
	e := echo.New()
	e.Logger.SetLevel(lblog.ERROR) // Set logging level to ERROR only

	// Recovery middleware for handling panics
	e.Use(middleware.Recover())

	e.GET("/getcredentials/:ConnectionName", GetCloudDriverAndConnectionInfoTLS)

	// Load CA certificate
	caCert, err := ioutil.ReadFile(caCertFile)
	if err != nil {
		fmt.Println("Failed to read CA certificate:", err)
		// return
	}

	// Set up CA certificate pool
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// Configure TLS settings
	tlsConfig := &tls.Config{
		ClientCAs:          caCertPool,
		ClientAuth:         tls.RequireAndVerifyClientCert,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: false,
	}

	// Bind to localhost only (127.0.0.1), external clients cannot connect
	address := fmt.Sprintf("127.0.0.1:%d", port)
	server := &http.Server{
		Addr:      address,
		Handler:   e,
		TLSConfig: tlsConfig,
	}

	fmt.Printf("[CB-Spider] TLS server running... https://%s\n", address)

	// Start TLS server
	err = server.ListenAndServeTLS(certFile, keyFile)
	if err != nil {
		fmt.Printf("[CB-Spider] Failed to start TLS server: %v\n", err)
	}
}

type bodyDumpResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w *bodyDumpResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

// ================ REST API Server: setup & start
func ApiServer(routes []route, s3Routes []route) {
	e := echo.New()

	// Middleware
	e.Use(middleware.CORS())
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	// Remove trailing slash middleware
	e.Pre(middleware.RemoveTrailingSlash())

	// Custom logging for S3 API requests
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if strings.HasPrefix(c.Request().Header.Get("Authorization"), "AWS4-HMAC-SHA256") {
				cblog.Infof("S3 API Request: %s %s", c.Request().Method, c.Request().URL.Path)
				cblog.Debugf("Request Headers: %v", c.Request().Header)

				// Capture the response body
				resBody := new(bytes.Buffer)
				mw := io.MultiWriter(c.Response().Writer, resBody)
				writer := &bodyDumpResponseWriter{Writer: mw, ResponseWriter: c.Response().Writer}
				c.Response().Writer = writer

				err := next(c)

				cblog.Debugf("Response Status: %d", c.Response().Status)
				cblog.Debugf("Response Headers: %v", c.Response().Header())
				if c.Response().Status < 300 {
					cblog.Debugf("Response Body: %s", resBody.String())
				}

				return err
			}
			return next(c)
		}
	})

	cbspiderRoot := os.Getenv("CBSPIDER_ROOT")

	// for HTTP Access Log
	e.Logger.SetOutput(&lumberjack.Logger{
		Filename:   cbspiderRoot + "/log/http-access.log",
		MaxSize:    10, // megabytes
		MaxBackups: 10, // number of backups
		MaxAge:     31, // days
	})

	API_USERNAME := os.Getenv("API_USERNAME")
	API_PASSWORD := os.Getenv("API_PASSWORD")

	// SkipAuthPaths defines paths to skip authentication
	SkipAuthPaths := map[string]bool{
		"/spider/version":     true,
		"/spider/healthcheck": true,
		"/spider/health":      true,
		"/spider/ping":        true,
		"/spider/readyz":      true,
	}

	if API_USERNAME != "" && API_PASSWORD != "" {
		cblog.Info("**** Rest Auth Enabled ****")
		e.Use(middleware.BasicAuthWithConfig(middleware.BasicAuthConfig{
			Skipper: func(c echo.Context) bool {
				return SkipAuthPaths[c.Path()]
			},
			Validator: func(username, password string, c echo.Context) (bool, error) {
				// Be careful to use constant time comparison to prevent timing attacks
				if subtle.ConstantTimeCompare([]byte(username), []byte(API_USERNAME)) == 1 &&
					subtle.ConstantTimeCompare([]byte(password), []byte(API_PASSWORD)) == 1 {
					return true, nil
				}
				return false, nil
			},
		}))
	} else {
		cblog.Info("**** Rest Auth Disabled ****")
	}

	for _, route := range routes {
		// /driver => /spider/driver
		route.path = "/spider" + route.path
		switch route.method {
		case "POST":
			e.POST(route.path, route.function)
		case "GET":
			e.GET(route.path, route.function)
		case "PUT":
			e.PUT(route.path, route.function)
		case "DELETE":
			e.DELETE(route.path, route.function)

		}
	}

	// Standard S3 API routes (root level)
	for _, route := range s3Routes {
		switch route.method {
		case "GET":
			e.GET(route.path, route.function)
		case "HEAD":
			e.HEAD(route.path, route.function)
		case "PUT":
			e.PUT(route.path, route.function)
		case "POST":
			e.POST(route.path, route.function)
		case "DELETE":
			e.DELETE(route.path, route.function)
		}
	}

	// Standard S3 API routes with /spider prefix
	for _, route := range s3Routes {
		spiderPath := "/spider" + route.path
		switch route.method {
		case "GET":
			e.GET(spiderPath, route.function)
		case "HEAD":
			e.HEAD(spiderPath, route.function)
		case "PUT":
			e.PUT(spiderPath, route.function)
		case "POST":
			e.POST(spiderPath, route.function)
		case "DELETE":
			e.DELETE(spiderPath, route.function)
		}
	}

	// for spider logo
	e.Static("/spider/adminweb/images", filepath.Join(cbspiderRoot, "api-runtime/rest-runtime/admin-web/images"))

	// for admin-web
	e.File("/spider/adminweb/html/priceinfo-filter-gen.html", cbspiderRoot+"/api-runtime/rest-runtime/admin-web/html/priceinfo-filter-gen.html")

	// for WebTerminal
	e.Static("/spider/adminweb/static", filepath.Join(cbspiderRoot, "api-runtime/rest-runtime/admin-web/static"))

	e.HideBanner = true
	e.HidePort = true

	spiderBanner()

	httpServerPort := cr.ServerPort
	if cr.ServerIPorName == "localhost" || cr.ServerIPorName == "127.0.0.1" {
		// Bind to localhost only (127.0.0.1), external clients cannot connect
		httpServerPort = cr.ServerIPorName + cr.ServerPort
	}

	server := &http.Server{
		Addr: httpServerPort,
		//ReadTimeout:    6000 * time.Second, // Increase the maximum duration of reading the entire request
		//WriteTimeout:   6000 * time.Second, // Increase the maximum duration of writing the entire response
		//IdleTimeout:    6000 * time.Second, // Increase the maximum duration of idle keep-alive connections
		MaxHeaderBytes: 500 * 1024 * 1024, // Increase the maximum header size allowed by the server
		ErrorLog:       log.New(os.Stderr, "HTTP SERVER ERROR: ", log.LstdFlags),
	}

	if err := e.StartServer(server); err != nil {
		cblog.Fatalf("Failed to start the server: %v", err)
	}
}

// ================ Endpoint Info
func endpointInfo(c echo.Context) error {
	cblog.Info("call endpointInfo()")

	endpointInfo := fmt.Sprintf("\n  <CB-Spider> Multi-Cloud Infrastructure Federation Framework\n")
	adminWebURL := "http://" + cr.ServiceIPorName + cr.ServicePort + "/spider/adminweb"
	endpointInfo += fmt.Sprintf("     - AdminWeb: %s\n", adminWebURL)
	swaggerURL := "http://" + cr.ServiceIPorName + cr.ServicePort + "/spider/api"
	endpointInfo += fmt.Sprintf("     - Swagger UI: %s\n", swaggerURL)

	// gRPCServer := "grpc://" + cr.ServiceIPorName + cr.GoServicePort
	// endpointInfo += fmt.Sprintf("     - Go   API: %s\n", gRPCServer)

	return c.String(http.StatusOK, endpointInfo)
}

// ================ Version Info
// func versionInfo(c echo.Context) error {
// 	cblog.Info("call versionInfo()")

// 	versionInfo := fmt.Sprintf("\n  <CB-Spider> Multi-Cloud Infrastructure Federation Framework\n")
// 	versionInfo += fmt.Sprintf("     - Version: %s\n", ar.Version)
// 	versionInfo += fmt.Sprintf("     - Git Commit SHA: %s\n", ar.CommitSHA)
// 	versionInfo += fmt.Sprintf("     - Build Timestamp: %s\n", ar.BuildTime)
// 	versionInfo += fmt.Sprintf("     - Server Started At: %s\n", cr.StartTime)

//		return c.String(http.StatusOK, versionInfo)
//	}
//
// VersionInfoResponse represents the response body for the versionInfo API.
type VersionInfoResponse struct {
	Version string `json:"Version" example:"CB-Spider v0.10.2-22"`
}

var spiderVersionInfo = VersionInfoResponse{}

func SetVersionInfo(version string) {
	spiderVersionInfo.Version = "CB-Spider " + version
}

// versionInfo godoc
// @ID version-info
// @Summary Get Version Information
// @Description Retrieves the version information of CB-Spider.
// @Tags [Version]
// @Accept  json
// @Produce  json
// @Success 200 {object} VersionInfoResponse "Version information retrieved successfully"
// @Failure 500 {object} SimpleMsg "Internal Server Error"
// @Router /version [get]
func versionInfo(c echo.Context) error {
	cblog.Info("call versionInfo()")

	return c.JSON(http.StatusOK, spiderVersionInfo)
}

// HealthCheckResponse represents the response body for the healthCheck API.
type HealthCheckResponse struct {
	Message string `json:"message" validate:"required" example:"CB-Spider is ready"`
}

// healthCheck godoc
// @ID health-check-healthcheck
// @Summary Perform Health Check
// @Description Checks the health of CB-Spider service and its dependencies via /healthcheck endpoint. 🕷️ [[User Guide](https://github.com/cloud-barista/cb-spider/wiki/Readiness-Check-Guide)]
// @Tags [Health Check]
// @Accept  json
// @Produce  json
// @Success 200 {object} HealthCheckResponse "Service is ready"
// @Failure 503 {object} SimpleMsg "Service Unavailable"
// @Router /healthcheck [get]
func healthCheckHealthCheck(c echo.Context) error {
	return healthCheck(c)
}

// healthCheck godoc
// @ID health-check-health
// @Summary Perform Health Check
// @Description Checks the health of CB-Spider service and its dependencies via /health endpoint. 🕷️ [[User Guide](https://github.com/cloud-barista/cb-spider/wiki/Readiness-Check-Guide)]
// @Tags [Health Check]
// @Accept  json
// @Produce  json
// @Success 200 {object} HealthCheckResponse "Service is ready"
// @Failure 503 {object} SimpleMsg "Service Unavailable"
// @Router /health [get]
func healthCheckHealth(c echo.Context) error {
	return healthCheck(c)
}

// healthCheck godoc
// @ID health-check-ping
// @Summary Perform Health Check
// @Description Checks the health of CB-Spider service and its dependencies via /ping endpoint. 🕷️ [[User Guide](https://github.com/cloud-barista/cb-spider/wiki/Readiness-Check-Guide)]
// @Tags [Health Check]
// @Accept  json
// @Produce  json
// @Success 200 {object} HealthCheckResponse "Service is ready"
// @Failure 503 {object} SimpleMsg "Service Unavailable"
// @Router /ping [get]
func healthCheckPing(c echo.Context) error {
	return healthCheck(c)
}

// healthCheck godoc
// @ID health-check-readyz
// @Summary Perform Health Check
// @Description Checks the health of CB-Spider service and its dependencies via /readyz endpoint. 🕷️ [[User Guide](https://github.com/cloud-barista/cb-spider/wiki/Readiness-Check-Guide)]
// @Tags [Health Check]
// @Accept  json
// @Produce  json
// @Success 200 {object} HealthCheckResponse "Service is ready"
// @Failure 503 {object} SimpleMsg "Service Unavailable"
// @Router /readyz [get]
func healthCheckReadyz(c echo.Context) error {
	return healthCheck(c)
}

func customRemoveTrailingSlash() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			url := req.URL
			path := url.Path

			if strings.HasPrefix(path, "/spider/api") {
				return next(c)
			}

			if len(path) > 1 && strings.HasSuffix(path, "/") {
				redirectPath := path[:len(path)-1]
				if url.RawQuery != "" {
					redirectPath += "?" + url.RawQuery
				}
				return c.Redirect(http.StatusMovedPermanently, redirectPath)
			}

			return next(c)
		}
	}
}

// Common health check logic
func healthCheck(c echo.Context) error {
	// check database connection
	err := infostore.Ping()
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "CB-Spider is ready"})
}

func spiderBanner() {
	fmt.Println("\n  <CB-Spider> Multi-Cloud Infrastructure Federation Framework")

	// AdminWeb
	adminWebURL := "http://" + cr.ServiceIPorName + cr.ServicePort + "/spider/adminweb"
	fmt.Printf("     - AdminWeb: %s\n", adminWebURL)

	// Swagger
	swaggerURL := "http://" + cr.ServiceIPorName + cr.ServicePort + "/spider/api"
	fmt.Printf("     - Swagger UI: %s\n", swaggerURL)

}
