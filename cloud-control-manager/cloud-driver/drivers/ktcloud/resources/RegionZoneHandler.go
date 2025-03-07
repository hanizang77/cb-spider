// Proof of Concepts for the Cloud-Barista Multi-Cloud Project.
//      * Cloud-Barista: https://github.com/cloud-barista
//
// KT Cloud RegionZone Handler
//
// Created by ETRI, 2023.10.
// Updated by ETRI, 2025.02.
//==================================================================================================

package resources

import (
	"fmt"
	"sync"
	"strings"
	// "errors"
	// "github.com/davecgh/go-spew/spew"

	ktsdk "github.com/cloud-barista/ktcloud-sdk-go"

	call "github.com/cloud-barista/cb-spider/cloud-control-manager/cloud-driver/call-log"
	idrv "github.com/cloud-barista/cb-spider/cloud-control-manager/cloud-driver/interfaces"
	irs "github.com/cloud-barista/cb-spider/cloud-control-manager/cloud-driver/interfaces/resources"
)

const (
	API_v1 string = "https://api.ucloudbiz.olleh.com/server/v1/client/api"
	API_v2 string = "https://api.ucloudbiz.olleh.com/server/v2/client/api" // When Zone is 'KOR-Seoul M2' => uses API v2
)

type KtRegionInfo struct {
    RegionCode 	string
    RegionName	string
}

// [Caution!!]
// In the KT Cloud, only Zones are present without Region information, so this connection driver creates and uses the Region information.
// The 'RegionCode' below is created using KT Cloud Zone Name. 'RegoneCode' string should be contained in the KT Cloud Zone Name.
// If the Zone of New Region available in KT Cloud is added, the Region should be added below.
func getSupportedRegions() []KtRegionInfo {
	regionInfoList := []KtRegionInfo {
		{	RegionCode: 	"KOR-Seoul",
			RegionName: 	"Seoul",
		},
		{	RegionCode: 	"KOR-Central",
			RegionName: 	"Cheonan",
		},
	}
	return regionInfoList
}

type KtCloudRegionZoneHandler struct {
	CredentialInfo 	idrv.CredentialInfo
	RegionInfo    	idrv.RegionInfo
	Client      	*ktsdk.KtCloudClient
}

func (regionZoneHandler *KtCloudRegionZoneHandler) ListRegionZone() ([]*irs.RegionZoneInfo, error) {
	cblogger.Info("KT Cloud Driver: called ListRegionZone()!!")
	InitLog()
	callLogInfo := GetCallLogScheme(regionZoneHandler.RegionInfo.Zone, call.REGIONZONE, "ListRegionZone()", "ListRegionZone()")

	ktRegionList := getSupportedRegions()
	if len(ktRegionList) < 1 {
		rtnErr := logAndReturnError(callLogInfo, "Failed to Find Any Region Info.", "")
		return nil, rtnErr
	}

	// ### Even though KT Cloud does not provide Region info., to get the Zonelist of 'All' Region.
	var regionZoneInfoList []*irs.RegionZoneInfo
	var wait sync.WaitGroup
	var zoneInfoListError error
	for _, region := range ktRegionList {
		wait.Add(1)
		go func(region KtRegionInfo) {
			defer wait.Done()
			cblogger.Info("# KT RegionCode : ", region.RegionCode)

			regionZoneInfo := irs.RegionZoneInfo{
				Name: 			region.RegionCode,
				DisplayName: 	region.RegionName,
				CSPDisplayName:	region.RegionName,
			}

			zoneInfoList, err := regionZoneHandler.getZoneInfoList(region.RegionCode)
			if err != nil {
				zoneInfoListError = err
				return
			}
			regionZoneInfo.ZoneList = zoneInfoList			
			regionZoneInfoList = append(regionZoneInfoList, &regionZoneInfo)
		}(region)

	}
	wait.Wait()

	if zoneInfoListError != nil {
		rtnErr := logAndReturnError(callLogInfo, "Failed to Get ZoneInfoList!!", zoneInfoListError)
		return nil, rtnErr
	}

	return regionZoneInfoList, nil
}

func (regionZoneHandler KtCloudRegionZoneHandler) GetRegionZone(regionCode string) (irs.RegionZoneInfo, error) {
	cblogger.Info("KT Cloud Driver: called GetRegionZone()!!")	
	InitLog()
	callLogInfo := GetCallLogScheme(regionZoneHandler.RegionInfo.Zone, call.REGIONZONE, regionCode, "GetRegionZone()")

	if strings.EqualFold(regionCode, "") {
		rtnErr := logAndReturnError(callLogInfo, "Invalid RegionCode!!", "")
		return irs.RegionZoneInfo{}, rtnErr
	}

	ktRegionList := getSupportedRegions()
	if len(ktRegionList) < 1 {
		rtnErr := logAndReturnError(callLogInfo, "Failed to Find Any Region Info.", "")
		return irs.RegionZoneInfo{}, rtnErr
	}

	validRegionCode, validErr := regionZoneHandler.checkRegionCode(regionCode)
	if validErr != nil {
		rtnErr := logAndReturnError(callLogInfo, "Failed to Get Valid RegionCode :", validErr)
		return irs.RegionZoneInfo{}, rtnErr
	}

	var regionZoneInfo irs.RegionZoneInfo
	for _, region := range ktRegionList {
		// cblogger.Info("# KT RegionCode : ", region.RegionCode)
		if strings.EqualFold(validRegionCode, region.RegionCode) {
			regionZoneInfo = irs.RegionZoneInfo {
				Name: 			region.RegionCode,
				DisplayName: 	region.RegionName,
				CSPDisplayName:	region.RegionName,
			}
			break
		}		
	}

	zoneInfoList, err := regionZoneHandler.getZoneInfoList(validRegionCode)
	if err != nil {
		rtnErr := logAndReturnError(callLogInfo, "Failed to Get ZoneInfoList :", err)
		return irs.RegionZoneInfo{}, rtnErr
	}
	regionZoneInfo.ZoneList = zoneInfoList
	return regionZoneInfo, nil
}

func (regionZoneHandler *KtCloudRegionZoneHandler) ListOrgRegion() (string, error) {
	cblogger.Info("KT Cloud Driver: called ListOrgRegion()!!")
	InitLog()
	callLogInfo := GetCallLogScheme(regionZoneHandler.RegionInfo.Zone, call.REGIONZONE, "ListOrgRegion()", "ListOrgRegion()")

	// To return the results with a style similar to other CSPs.
	type Regions struct {
		RegionList 	[]KtRegionInfo
	}

	ktRegionList := getSupportedRegions()
	if len(ktRegionList) < 1 {
		rtnErr := logAndReturnError(callLogInfo, "Failed to Find Any Region Info.", "")
		return "", rtnErr
	}

	regionList := Regions{
		RegionList: ktRegionList,
	}
	jsonString, err := ConvertJsonString(regionList)
	if err != nil {
		rtnErr := logAndReturnError(callLogInfo, "Failed to Convert to Json String :", err)
		return "", rtnErr
	}
	return jsonString, nil
}

func (regionZoneHandler *KtCloudRegionZoneHandler) ListOrgZone() (string, error) {
	cblogger.Info("KT Cloud Driver: called ListOrgZone()!!")
	InitLog()
	callLogInfo := GetCallLogScheme(regionZoneHandler.RegionInfo.Zone, call.REGIONZONE, regionZoneHandler.RegionInfo.Region, "ListOrgZone()")

	// To return the results with a style similar to other CSPs.
	type Zones struct {
		ZoneList 	[]ktsdk.Zone
	}

	validRegionCode, validErr := regionZoneHandler.checkRegionCode(regionZoneHandler.RegionInfo.Region)
	if validErr != nil {
		rtnErr := logAndReturnError(callLogInfo, "Failed to Get Valid RegionCode :", validErr)
		return "", rtnErr
	}

	ktZoneList, err := regionZoneHandler.getKtZoneList(validRegionCode)
	if err != nil {
		rtnErr := logAndReturnError(callLogInfo, "Failed to Get ZoneInfoList :", err)
		return "", rtnErr
	}
	zoneList := Zones{
		ZoneList: ktZoneList,
	}
	jsonString, err := ConvertJsonString(zoneList)
	if err != nil {
		rtnErr := logAndReturnError(callLogInfo, "Failed to Convert to Json String :", err)
		return "", rtnErr
	}
	return jsonString, nil
}

func (regionZoneHandler KtCloudRegionZoneHandler) getZoneInfoList(regionCode string) ([]irs.ZoneInfo, error) {
	cblogger.Info("KT Cloud VPC Driver: called getZoneInfoList()!!")

	if strings.EqualFold(regionCode, "") {
		newErr := fmt.Errorf("Invalid RegionCode!!")
		cblogger.Error(newErr.Error())
		return nil, newErr
	}

	if strings.EqualFold(regionZoneHandler.CredentialInfo.ClientId, "") {
		newErr := fmt.Errorf("Invalid ClientId!!")
		cblogger.Error(newErr.Error())
		return nil, newErr
	}

	if strings.EqualFold(regionZoneHandler.CredentialInfo.ClientSecret, "") {
		newErr := fmt.Errorf("Invalid ClientSecret!!")
		cblogger.Error(newErr.Error())
		return nil, newErr
	}

	ktZoneList, err := regionZoneHandler.getKtZoneList(regionCode)
	if err != nil {
		newErr := fmt.Errorf("Failed to Get KT Cloud ZoneList : [%v]", err)
		cblogger.Error(newErr.Error())
		return nil, newErr
	}

	var zoneInfoList []irs.ZoneInfo
	for _, zone := range ktZoneList {
		zoneInfo := irs.ZoneInfo{
			Name: 			zone.ID,
			DisplayName: 	zone.Name,
			CSPDisplayName: zone.Name,
		}			
		if strings.EqualFold(zone.AllocationState, "Enabled") {
			zoneInfo.Status = irs.ZoneAvailable
		} else {
			zoneInfo.Status = irs.ZoneUnavailable
		}
		zoneInfoList = append(zoneInfoList, zoneInfo)			
	}
	return zoneInfoList, nil
}

func (regionZoneHandler KtCloudRegionZoneHandler) getKtZoneList(regionCode string) ([]ktsdk.Zone, error) {
	cblogger.Info("KT Cloud Driver: called getKtZoneList()!!")

	if strings.EqualFold(regionCode, "") {
		newErr := fmt.Errorf("Invalid RegionCode!!")
		cblogger.Error(newErr.Error())
		return nil, newErr
	}

	if strings.EqualFold(regionZoneHandler.CredentialInfo.ClientId, "") {
		newErr := fmt.Errorf("Invalid ClientId!!")
		cblogger.Error(newErr.Error())
		return nil, newErr
	}

	if strings.EqualFold(regionZoneHandler.CredentialInfo.ClientSecret, "") {
		newErr := fmt.Errorf("Invalid ClientSecret!!")
		cblogger.Error(newErr.Error())
		return nil, newErr
	}

	var zoneList []ktsdk.Zone
	apiUrlList := make([]string, 2) 	// Not : var apiList []string
	apiUrlList[0] = API_v1
	apiUrlList[1] = API_v2 // When Zone is 'KOR-Seoul M2' => uses API v2

	for _, apiUrl := range apiUrlList {		
		// Always validate any SSL certificates in the chain
		insecureSkipVerify := false
		cs := ktsdk.KtCloudClient{}.New(apiUrl, regionZoneHandler.CredentialInfo.ClientId, regionZoneHandler.CredentialInfo.ClientSecret, insecureSkipVerify)

		// # The first (isAvailble) parameter of ListZones() method : 
			// All available zone information inquiry : true (default)
			// ZONE info. inquiry with at least one 'VM' : false
		response, err := cs.ListZones(true, "", "", "")
		if err != nil {
			newErr := fmt.Errorf("Failed to Get Available Zone List : [%v]", err)
			cblogger.Error(newErr.Error())
			return nil, newErr
		}

		for _, zone := range response.Listzonesresponse.Zone {
			if strings.Contains(zone.Name, regionCode) {  // Caution!!
				// cblogger.Info("# KT Zone Name : ", zone.Name)
				zoneList = append(zoneList, zone)		
			}
		}
	}
	return zoneList, nil
}

// RegionCode Validation Check
func (regionZoneHandler KtCloudRegionZoneHandler) checkRegionCode(regionCode string) (string, error) {
	cblogger.Info("KT Cloud Driver: called checkRegionCode()!!")	
	InitLog()
	callLogInfo := GetCallLogScheme(regionZoneHandler.RegionInfo.Zone, call.REGIONZONE, regionCode, "checkRegionCode()")

	if strings.EqualFold(regionCode, "") {
		rtnErr := logAndReturnError(callLogInfo, "Invalid RegionCode!!", "")
		return "", rtnErr
	}

	ktRegionList := getSupportedRegions()
	if len(ktRegionList) < 1 {
		rtnErr := logAndReturnError(callLogInfo, "Failed to Find Any Region Info.", "")
		return "", rtnErr
	}

	var valideRegionCode string
	for _, region := range ktRegionList {
		// cblogger.Info("# KT RegionCode : ", region.RegionCode)
		if strings.EqualFold(regionCode, region.RegionCode) {
			valideRegionCode = region.RegionCode
			break
		}
	}

	if strings.EqualFold(valideRegionCode, "") {
		rtnErr := logAndReturnError(callLogInfo, "The RegionCode are Not Exist!!", "")
		return "", rtnErr
	}

	return valideRegionCode, nil
}
