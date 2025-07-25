// Cloud ConnectionConfig Info. Manager of CB-Spider.
// The CB-Spider is a sub-Framework of the Cloud-Barista Multi-Cloud Project.
// The CB-Spider Mission is to connect all the clouds with a single interface.
//
//      * Cloud-Barista: https://github.com/cloud-barista
//
// by CB-Spider Team, 2019.09.

package connectionconfiginfomanager

import (
	"fmt"
	"strings"

	cblogger "github.com/cloud-barista/cb-log"
	"github.com/sirupsen/logrus"

	infostore "github.com/cloud-barista/cb-spider/info-store"
)

// ====================================================================
const KEY_COLUMN_NAME = "config_name"

// ConnectionConfigInfo represents the configuration information for cloud connection.
// @Description Information about the connection configuration used to connect to a specific cloud provider.
type ConnectionConfigInfo struct {
	ConfigName     string `json:"ConfigName" gorm:"primaryKey" validate:"required" example:"config01"` // The name of the connection configuration, used as a unique identifier.
	ProviderName   string `json:"ProviderName" validate:"required" example:"AWS"`                      // The name of the cloud provider (e.g., AWS, Azure, GCP).
	DriverName     string `json:"DriverName" validate:"required" example:"AWS-Test-Driver-V0.5"`       // The name of the cloud driver associated with this configuration.
	CredentialName string `json:"CredentialName" validate:"required" example:"credential01"`           // The name of the credential used for cloud authentication.
	RegionName     string `json:"RegionName" validate:"required" example:"region01"`                   // The name of the region for the cloud connection.
}

//====================================================================

var cblog *logrus.Logger

func init() {
	cblog = cblogger.GetLogger("CLOUD-BARISTA")

	db, err := infostore.Open()
	if err != nil {
		panic("Failed to Connect to Database")
	}
	db.AutoMigrate(&ConnectionConfigInfo{})
	infostore.Close(db)
}

// 1. check params
// 2. insert them into info-store
func CreateConnectionConfigInfo(configInfo ConnectionConfigInfo) (*ConnectionConfigInfo, error) {
	cblog.Info("call CreateConnectionConfigInfo()")

	cblog.Debug("check params")
	err := checkParams(configInfo.ConfigName,
		configInfo.ProviderName, configInfo.DriverName, configInfo.CredentialName, configInfo.RegionName)
	if err != nil {
		return nil, err

	}

	// trim user inputs
	configInfo.ConfigName = strings.TrimSpace(configInfo.ConfigName)
	configInfo.ProviderName = strings.ToUpper(strings.TrimSpace(configInfo.ProviderName))
	configInfo.DriverName = strings.TrimSpace(configInfo.DriverName)
	configInfo.CredentialName = strings.TrimSpace(configInfo.CredentialName)
	configInfo.RegionName = strings.TrimSpace(configInfo.RegionName)

	cblog.Debug("insert metainfo into store")

	err = infostore.Insert(&configInfo)
	if err != nil {
		cblog.Error(err)
		return nil, err
	}

	return &configInfo, nil
}

func CreateConnectionConfig(configName string,
	providerName string, driverName string, credentialName string, regionName string) (*ConnectionConfigInfo, error) {
	cblog.Info("call CreateConnectionConfig()")
	return CreateConnectionConfigInfo(ConnectionConfigInfo{configName,
		providerName, driverName, credentialName, regionName})
}

func ListConnectionConfig() ([]*ConnectionConfigInfo, error) {
	// cblog.Info("call ListConnectionConfig()")

	var connectionConfigInfoList []*ConnectionConfigInfo
	err := infostore.List(&connectionConfigInfoList)
	if err != nil {
		return nil, err
	}

	return connectionConfigInfoList, nil
}

// 1. check params
// 2. get ConnectionConfigInfo from info-store
func GetConnectionConfig(configName string) (*ConnectionConfigInfo, error) {
	cblog.Info("call GetConnectionConfig()")

	if configName == "" {
		return nil, fmt.Errorf("ConfigName is empty!")
	}

	var connectionConfigInfo ConnectionConfigInfo
	err := infostore.Get(&connectionConfigInfo, KEY_COLUMN_NAME, configName)
	if err != nil {
		cblog.Error(err)
		return nil, err
	}

	return &connectionConfigInfo, err
}

func DeleteConnectionConfig(configName string) (bool, error) {
	cblog.Info("call DeleteConnectionConfig()")

	if configName == "" {
		return false, fmt.Errorf("ConfigName is empty!")
	}

	result, err := infostore.Delete(&ConnectionConfigInfo{}, KEY_COLUMN_NAME, configName)
	if err != nil {
		cblog.Error(err)
		return false, err
	}

	return result, nil
}

//----------------

func checkParams(configName string, providerName string, driverName string, credentialName string, regionName string) error {
	if configName == "" {
		return fmt.Errorf("ConfigName is empty!")
	}
	if providerName == "" {
		return fmt.Errorf("ProviderName is empty!")
	}
	if driverName == "" {
		return fmt.Errorf("DriverName is empty!")
	}
	if credentialName == "" {
		return fmt.Errorf("CredentialName is empty!")
	}
	if regionName == "" {
		return fmt.Errorf("RegionName is empty!")
	}

	return nil
}

func CountAllConnections() (int64, error) {
	cblog.Info("call ListConnectionConfig()")

	var connectionConfigInfo ConnectionConfigInfo
	count, err := infostore.CountAllConnections(&connectionConfigInfo)
	if err != nil {
		cblog.Error(err)
		return count, err
	}

	return count, nil
}

func CountConnectionsByProvider(providerName string) (int64, error) {
	providerName = strings.ToUpper(strings.TrimSpace(providerName))

	var connectionConfigInfo ConnectionConfigInfo
	count, err := infostore.CountConnectionsByProvider(&connectionConfigInfo, providerName)
	if err != nil {
		cblog.Error(err)
		return count, err
	}

	return count, nil
}
