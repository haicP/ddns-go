package dns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jeessy2/ddns-go/v6/config"
	"github.com/jeessy2/ddns-go/v6/util"
)

const (
	esaEndpoint string = "https://esa.cn-hangzhou.aliyuncs.com/"
)

// ESA Alibaba Cloud ESA
type ESA struct {
	DNS     config.DNS
	Domains config.Domains
	TTL     string
}

// ESARecord record
type ESARecord struct {
	RecordId   int64
	RecordName string
	Type       string
	Data       struct {
		Value string
	}
}

// ESASite site
type ESASite struct {
	SiteId   int64
	SiteName string
}

// ESAListSitesResp list sites response
type ESAListSitesResp struct {
	TotalCount int
	Sites      []ESASite
}

// ESAListRecordsResp list records response
type ESAListRecordsResp struct {
	TotalCount int
	Records    []ESARecord
}

// ESAResp generic response with RequestId
type ESAResp struct {
	RequestId string
	RecordId  int64 // For CreateRecord/UpdateRecord
}

// Init initialization
func (esa *ESA) Init(dnsConf *config.DnsConfig, ipv4cache *util.IpCache, ipv6cache *util.IpCache) {
	esa.Domains.Ipv4Cache = ipv4cache
	esa.Domains.Ipv6Cache = ipv6cache
	esa.DNS = dnsConf.DNS
	esa.Domains.GetNewIp(dnsConf)
	if dnsConf.TTL == "" {
		// Default to 1 (automatic) or 600? API says 30~86400 or 1.
		esa.TTL = "30"
	} else {
		esa.TTL = dnsConf.TTL
	}
}

// AddUpdateDomainRecords add or update IPv4/IPv6 records
func (esa *ESA) AddUpdateDomainRecords() config.Domains {
	esa.addUpdateDomainRecords("A")
	esa.addUpdateDomainRecords("AAAA")
	return esa.Domains
}

func (esa *ESA) addUpdateDomainRecords(recordType string) {
	ipAddr, domains := esa.Domains.GetNewIpResult(recordType)

	if ipAddr == "" {
		return
	}

	for _, domain := range domains {
		// Get SiteId
		siteId, err := esa.getSiteId(domain.DomainName)
		if err != nil {
			util.Log("Failed to get Site ID for %s: %s", domain.DomainName, err)
			domain.UpdateStatus = config.UpdatedFailed
			continue
		}

		// List existing records
		records, err := esa.listRecords(siteId, domain, recordType)
		if err != nil {
			util.Log("Failed to list records for %s: %s", domain.GetFullDomain(), err)
			domain.UpdateStatus = config.UpdatedFailed
			continue
		}

		if len(records) > 0 {
			// Update existing record
			// Assuming we update the first matching record if multiple exist
			esa.modify(siteId, records[0], domain, recordType, ipAddr)
		} else {
			// Create new record
			esa.create(siteId, domain, recordType, ipAddr)
		}
	}
}

func (esa *ESA) getSiteId(domainName string) (int64, error) {
	params := url.Values{}
	params.Set("Action", "ListSites")
	params.Set("Version", "2024-09-10")
	params.Set("SiteName", domainName)
	params.Set("ExactMatch", "true") // Ensure exact match

	var result ESAListSitesResp
	err := esa.request(params, &result)
	if err != nil {
		return 0, err
	}

	if result.TotalCount == 0 || len(result.Sites) == 0 {
		return 0, fmt.Errorf("site not found for domain: %s", domainName)
	}

	return result.Sites[0].SiteId, nil
}

func (esa *ESA) listRecords(siteId int64, domain *config.Domain, recordType string) ([]ESARecord, error) {
	params := url.Values{}
	params.Set("Action", "ListRecords")
	params.Set("Version", "2024-09-10")
	params.Set("SiteId", strconv.FormatInt(siteId, 10))
	params.Set("RecordName", domain.GetFullDomain())
	params.Set("RecordNameMode", "exact")
	params.Set("Type", recordType)

	var result ESAListRecordsResp
	err := esa.request(params, &result)
	if err != nil {
		return nil, err
	}

	return result.Records, nil
}

func (esa *ESA) create(siteId int64, domain *config.Domain, recordType string, ipAddr string) {
	params := domain.GetCustomParams()
	params.Set("Action", "CreateRecord")
	params.Set("Version", "2024-09-10")
	params.Set("SiteId", strconv.FormatInt(siteId, 10))
	params.Set("RecordName", domain.GetFullDomain())
	params.Set("Type", recordType)
	
	// Construct Data JSON
	data := map[string]string{
		"Value": ipAddr,
	}
	dataBytes, _ := json.Marshal(data)
	params.Set("Data", string(dataBytes))
	
	params.Set("TTL", esa.TTL)

	var result ESAResp
	err := esa.request(params, &result)

	if err != nil {
		util.Log("新增域名解析 %s 失败! 异常信息: %s", domain, err)
		domain.UpdateStatus = config.UpdatedFailed
		return
	}

	// CreateRecord response doesn't strictly guarantee RecordId presence in all APIs, 
    // but usually it returns it. The struct field int defaults to 0.
    // If successful, error should be nil.
	util.Log("新增域名解析 %s 成功! IP: %s", domain, ipAddr)
	domain.UpdateStatus = config.UpdatedSuccess
}

func (esa *ESA) modify(siteId int64, record ESARecord, domain *config.Domain, recordType string, ipAddr string) {
	if record.Data.Value == ipAddr {
		util.Log("你的IP %s 没有变化, 域名 %s", ipAddr, domain)
		return
	}

	params := domain.GetCustomParams()
	params.Set("Action", "UpdateRecord")
	params.Set("Version", "2024-09-10")
	params.Set("SiteId", strconv.FormatInt(siteId, 10))
	params.Set("RecordId", strconv.FormatInt(record.RecordId, 10))
	params.Set("RecordName", domain.GetFullDomain()) // Some APIs require this even for update
	params.Set("Type", recordType)
    
	// Construct Data JSON
	data := map[string]string{
		"Value": ipAddr,
	}
	dataBytes, _ := json.Marshal(data)
	params.Set("Data", string(dataBytes))
    
    // Use configured TTL or default
	params.Set("TTL", esa.TTL)

	var result ESAResp
	err := esa.request(params, &result)

	if err != nil {
		util.Log("更新域名解析 %s 失败! 异常信息: %s", domain, err)
		domain.UpdateStatus = config.UpdatedFailed
		return
	}

	util.Log("更新域名解析 %s 成功! IP: %s", domain, ipAddr)
	domain.UpdateStatus = config.UpdatedSuccess
}

func (esa *ESA) request(params url.Values, result interface{}) error {
	util.AliyunSigner(esa.DNS.ID, esa.DNS.Secret, &params)

	req, err := http.NewRequest(
		"GET",
		esaEndpoint,
		bytes.NewBuffer(nil),
	)
	if err != nil {
		return err
	}
	
	req.URL.RawQuery = params.Encode()

	client := util.CreateHTTPClient()
	resp, err := client.Do(req)
	return util.GetHTTPResponse(resp, err, result)
}
