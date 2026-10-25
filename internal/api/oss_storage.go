package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

type OSSConfig struct {
	Enabled            bool   `json:"enabled"`
	Bucket             string `json:"bucket"`
	Region             string `json:"region"`
	Endpoint           string `json:"endpoint"`
	AccelerateEndpoint string `json:"accelerateEndpoint"`
	UploadAccelerate   bool   `json:"uploadAccelerate"`
	ReadAccelerate     bool   `json:"readAccelerate"`
	Prefix             string `json:"prefix"`
	CDNDomain          string `json:"cdnDomain"`
	PublicRead         bool   `json:"publicRead"`
	URLTTL             int    `json:"urlTTL"`
	AccessKey          string `json:"-"`
	SecretKey          string `json:"-"`
}

func defaultOSSConfig(production bool) OSSConfig {
	driver := "local"
	if production {
		driver = "oss"
	}
	ttl, _ := strconv.Atoi(env("OSS_URL_TTL", "3600"))
	return OSSConfig{Enabled: env("STORAGE_DRIVER", driver) == "oss", Bucket: env("OSS_BUCKET", "flowlinght"), Region: env("OSS_REGION", "cn-shanghai"), Endpoint: env("OSS_ENDPOINT", "https://oss-cn-shanghai.aliyuncs.com"), AccelerateEndpoint: env("OSS_ACCELERATE_ENDPOINT", "https://flowlinght.oss-accelerate.aliyuncs.com"), UploadAccelerate: env("OSS_UPLOAD_ACCELERATE", "true") == "true", ReadAccelerate: env("OSS_READ_ACCELERATE", "true") == "true", Prefix: env("OSS_PREFIX", "canvas/uploads/"), CDNDomain: env("OSS_CDN_DOMAIN", ""), PublicRead: env("OSS_PUBLIC_READ", "false") == "true", URLTTL: ttl, AccessKey: env("OSS_ACCESS_KEY_ID", ""), SecretKey: env("OSS_ACCESS_KEY_SECRET", "")}
}

func (a *App) storageConfig(s Store) (OSSConfig, error) {
	c := a.Config.OSS
	if c.Bucket == "" {
		c = defaultOSSConfig(a.Config.Production)
	}
	if row := s.One("SELECT config,credentials FROM object_storage WHERE id=1"); row != nil {
		if err := json.Unmarshal([]byte(str(row["config"])), &c); err != nil {
			return c, fmt.Errorf("OSS 配置无法读取")
		}
		plain, err := a.decrypt(str(row["credentials"]))
		if err != nil {
			return c, fmt.Errorf("OSS 密钥无法解密")
		}
		pair := object(plain)
		c.AccessKey, c.SecretKey = str(pair["accessKeyId"]), str(pair["accessKeySecret"])
	}
	return c, nil
}

func storageView(c OSSConfig) Row {
	v := object(jsonText(c))
	v["hasCredentials"] = c.AccessKey != "" && c.SecretKey != ""
	return v
}

func validateOSS(c OSSConfig, requireCredentials bool) error {
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`).MatchString(c.Bucket) {
		return fmt.Errorf("OSS Bucket 格式不正确")
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,60}$`).MatchString(c.Region) {
		return fmt.Errorf("OSS Region 格式不正确")
	}
	for _, value := range []string{c.Endpoint, c.AccelerateEndpoint, c.CDNDomain} {
		if value == "" {
			continue
		}
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("OSS 地址和 CDN 域名须为不带路径的 HTTPS 地址")
		}
	}
	if c.Endpoint == "" || ((c.UploadAccelerate || c.ReadAccelerate) && c.AccelerateEndpoint == "") {
		return fmt.Errorf("OSS Endpoint 或加速域名未配置")
	}
	if strings.ContainsAny(c.Prefix, "\\?#\r\n") || strings.HasPrefix(c.Prefix, "/") || len(c.Prefix) > 200 {
		return fmt.Errorf("OSS 目录前缀不正确")
	}
	for _, segment := range strings.Split(c.Prefix, "/") {
		if segment == ".." || segment == "." {
			return fmt.Errorf("OSS 目录前缀不能包含相对路径")
		}
	}
	if c.URLTTL < 60 || c.URLTTL > 604800 {
		return fmt.Errorf("OSS 链接有效期应为 60–604800 秒")
	}
	if requireCredentials && (c.AccessKey == "" || c.SecretKey == "") {
		return fmt.Errorf("OSS 尚未配置 AccessKey ID 和 AccessKey Secret")
	}
	return nil
}

func (a *App) parseStorage(s Store, body Row) OSSConfig {
	c, err := a.storageConfig(s)
	must(err)
	c.Enabled = boolField(body, "enabled", c.Enabled)
	c.Bucket = trimField(body, "bucket", 3, 63, c.Bucket)
	c.Region = trimField(body, "region", 1, 64, c.Region)
	c.Endpoint = strings.TrimRight(textField(body, "endpoint", 0, 500, c.Endpoint), "/")
	c.AccelerateEndpoint = strings.TrimRight(textField(body, "accelerateEndpoint", 0, 500, c.AccelerateEndpoint), "/")
	c.CDNDomain = strings.TrimRight(textField(body, "cdnDomain", 0, 500, c.CDNDomain), "/")
	c.Prefix = textField(body, "prefix", 0, 200, c.Prefix)
	if c.Prefix != "" {
		c.Prefix = strings.TrimRight(c.Prefix, "/") + "/"
	}
	c.UploadAccelerate = boolField(body, "uploadAccelerate", c.UploadAccelerate)
	c.ReadAccelerate = boolField(body, "readAccelerate", c.ReadAccelerate)
	c.PublicRead = boolField(body, "publicRead", c.PublicRead)
	ttl := numberField(body, "urlTTL", 60, 604800, float64(c.URLTTL))
	if ttl != float64(int(ttl)) {
		fail(400, "链接有效期必须为整数秒")
	}
	c.URLTTL = int(ttl)
	ak, sk := strings.TrimSpace(textField(body, "accessKeyId", 0, 256, "")), strings.TrimSpace(textField(body, "accessKeySecret", 0, 256, ""))
	if ak != "" || sk != "" {
		if ak == "" || sk == "" {
			fail(400, "更换密钥时请同时填写 AccessKey ID 和 Secret")
		}
		c.AccessKey, c.SecretKey = ak, sk
	}
	if boolField(body, "clearCredentials", false) {
		if c.Enabled {
			fail(400, "请先停用 OSS 再清除密钥")
		}
		c.AccessKey, c.SecretKey = "", ""
	}
	if err = validateOSS(c, c.Enabled); err != nil {
		fail(400, err.Error())
	}
	return c
}

func (a *App) ossClient(c OSSConfig, read, cdn bool) *oss.Client {
	endpoint, cname := c.Endpoint, false
	if (!read && c.UploadAccelerate) || (read && c.ReadAccelerate) {
		endpoint, cname = c.AccelerateEndpoint, true
	}
	if read && cdn && c.CDNDomain != "" {
		endpoint, cname = c.CDNDomain, true
	}
	cfg := oss.LoadDefaultConfig().WithRegion(c.Region).WithEndpoint(endpoint).WithUseCName(cname).WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey)).WithRetryMaxAttempts(3).WithConnectTimeout(10 * time.Second).WithReadWriteTimeout(120 * time.Second)
	if a.OSSHTTP != nil {
		cfg.WithHttpClient(a.OSSHTTP)
	}
	return oss.NewClient(cfg)
}

func ossFailure(action string, err error) error {
	var service *oss.ServiceError
	if errors.As(err, &service) {
		code := service.Code
		if !regexp.MustCompile(`^[A-Za-z0-9]+$`).MatchString(code) {
			code = "RemoteError"
		}
		return fmt.Errorf("OSS %s失败（%s），请检查密钥权限、Bucket 和加速配置", action, code)
	}
	return fmt.Errorf("OSS %s失败，请检查存储配置或网络后重试", action)
}

func (a *App) ossURL(c OSSConfig, key string, useCDN bool) (string, error) {
	if c.PublicRead {
		base := c.Endpoint
		if c.ReadAccelerate {
			base = c.AccelerateEndpoint
		} else {
			u, _ := url.Parse(base)
			u.Host = c.Bucket + "." + u.Host
			base = u.String()
		}
		if useCDN && c.CDNDomain != "" {
			base = c.CDNDomain
		}
		u, err := url.Parse(base)
		if err != nil {
			return "", fmt.Errorf("OSS 读取域名不正确")
		}
		u.Path = "/" + key
		return u.String(), nil
	}
	if c.AccessKey == "" || c.SecretKey == "" {
		return "", fmt.Errorf("OSS 读取密钥未配置")
	}
	result, err := a.ossClient(c, true, useCDN).Presign(context.Background(), &oss.GetObjectRequest{Bucket: oss.Ptr(c.Bucket), Key: oss.Ptr(key)}, oss.PresignExpires(time.Duration(c.URLTTL)*time.Second))
	if err != nil {
		return "", ossFailure("生成读取链接", err)
	}
	return result.URL, nil
}

func remoteOSSConfig(current OSSConfig, remote Row) OSSConfig {
	c := current
	c.Bucket, c.Region, c.Endpoint = str(remote["bucket"]), str(remote["region"]), str(remote["endpoint"])
	c.AccelerateEndpoint = str(remote["accelerateEndpoint"])
	// Existing objects stay in their original bucket if administrators change destinations.
	if current.Bucket != c.Bucket || current.Endpoint != c.Endpoint {
		c.ReadAccelerate = truth(remote["readAccelerate"])
		c.UploadAccelerate = false
		c.CDNDomain = str(remote["cdnDomain"])
		c.PublicRead = truth(remote["publicRead"])
	}
	return c
}

func (a *App) ensureOSSAsset(ctx context.Context, c OSSConfig, asset Row) (Row, error) {
	if len(object(asset["remote_storage"])) > 0 {
		return asset, nil
	}
	if err := validateOSS(c, true); err != nil {
		return nil, err
	}
	key := c.Prefix + time.Now().UTC().Format("2006/01/02/") + str(asset["file"])
	_, err := a.ossClient(c, false, false).PutObjectFromFile(ctx, &oss.PutObjectRequest{Bucket: oss.Ptr(c.Bucket), Key: oss.Ptr(key), ContentType: oss.Ptr(str(asset["mime"]))}, a.filePath(str(asset["file"])))
	if err != nil {
		return nil, ossFailure("转存", err)
	}
	remote := Row{"provider": "oss", "bucket": c.Bucket, "region": c.Region, "endpoint": c.Endpoint, "accelerateEndpoint": c.AccelerateEndpoint, "objectKey": key, "readAccelerate": c.ReadAccelerate, "cdnDomain": c.CDNDomain, "publicRead": c.PublicRead}
	err = try(func() {
		if a.Store.Exec("UPDATE assets SET remote_storage=? WHERE id=? AND user_id=?", jsonText(remote), asset["id"], asset["user_id"]) == 0 {
			fail(409, "转存素材已被移除")
		}
	})
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = a.ossClient(c, false, false).DeleteObject(cleanup, &oss.DeleteObjectRequest{Bucket: oss.Ptr(c.Bucket), Key: oss.Ptr(key)})
		return nil, err
	}
	result := clone(asset)
	result["remote_storage"] = remote
	return result, nil
}

func (a *App) assetLinks(s Store, asset Row) Row {
	local := "/api/assets/" + str(asset["id"]) + "/file"
	links := Row{"url": local, "downloadUrl": local + "?download=1"}
	remote := object(asset["remote_storage"])
	if remote["provider"] != "oss" {
		return links
	}
	c, err := a.storageConfig(s)
	if err == nil {
		c = remoteOSSConfig(c, remote)
		if address, e := a.ossURL(c, str(remote["objectKey"]), true); e == nil {
			links["url"], links["storage"] = address, "oss"
			if !c.PublicRead {
				links["urlExpiresAt"] = time.Now().UTC().Add(time.Duration(c.URLTTL) * time.Second).Format(time.RFC3339)
			}
		}
	}
	return links
}

func (a *App) jobView(s Store, j Row) Row {
	v := s.JobView(j)
	outputs := []any{}
	for _, raw := range array(v["outputs"]) {
		out := clone(object(raw))
		asset := s.One("SELECT * FROM assets WHERE id=? AND user_id=?", out["assetId"], j["user_id"])
		if asset != nil {
			for k, value := range a.assetLinks(s, asset) {
				out[k] = value
			}
		} else {
			out["url"] = "/api/assets/" + str(out["assetId"]) + "/file"
			out["downloadUrl"] = str(out["url"]) + "?download=1"
		}
		outputs = append(outputs, out)
	}
	v["outputs"] = outputs
	return v
}

func (a *App) storeGeneratedOutputs(ctx context.Context, j Row, outputs []Row) ([]Row, error) {
	c, err := a.storageConfig(a.Store)
	if err != nil {
		return nil, err
	}
	if !c.Enabled {
		return outputs, nil
	}
	if err = validateOSS(c, true); err != nil {
		return nil, err
	}
	for i, output := range outputs {
		asset := a.Store.One("SELECT * FROM assets WHERE id=? AND user_id=? AND job_id=?", output["assetId"], j["user_id"], j["id"])
		if asset == nil {
			return nil, fmt.Errorf("OSS 转存文件不存在")
		}
		asset, err = a.ensureOSSAsset(ctx, c, asset)
		if err != nil {
			return nil, err
		}
		remote := object(asset["remote_storage"])
		address, err := a.ossURL(remoteOSSConfig(c, remote), str(remote["objectKey"]), true)
		if err != nil {
			return nil, err
		}
		outputs[i] = clone(output)
		outputs[i]["url"], outputs[i]["storage"] = address, "oss"
		outputs[i]["downloadUrl"] = "/api/assets/" + str(asset["id"]) + "/file?download=1"
	}
	return outputs, nil
}

func (a *App) removeOSSAsset(ctx context.Context, asset Row) error {
	remote := object(asset["remote_storage"])
	if remote["provider"] != "oss" {
		return nil
	}
	c, err := a.storageConfig(a.Store)
	if err != nil {
		return err
	}
	c = remoteOSSConfig(c, remote)
	_, err = a.ossClient(c, false, false).DeleteObject(ctx, &oss.DeleteObjectRequest{Bucket: oss.Ptr(c.Bucket), Key: oss.Ptr(str(remote["objectKey"]))})
	if err != nil {
		return ossFailure("删除文件", err)
	}
	return nil
}

func (a *App) storageRoutes(m *http.ServeMux) {
	a.route(m, "GET /api/admin/storage", 3, func(r *Request) any { c, err := a.storageConfig(a.Store); must(err); return storageView(c) })
	a.route(m, "PUT /api/admin/storage", 3, func(r *Request) any {
		body := r.Body()
		var result Row
		a.Tx(func(s Store) {
			c := a.parseStorage(s, body)
			pair := ""
			if c.AccessKey != "" {
				pair = a.encrypt(jsonText(Row{"accessKeyId": c.AccessKey, "accessKeySecret": c.SecretKey}))
			}
			s.Exec("INSERT INTO object_storage(id,config,credentials) VALUES(1,?,?) ON DUPLICATE KEY UPDATE config=VALUES(config),credentials=VALUES(credentials)", jsonText(c), pair)
			s.Audit(r.UID(), "storage.update", Row{"enabled": c.Enabled, "bucket": c.Bucket, "cdnDomain": c.CDNDomain, "credentialsChanged": str(body["accessKeyId"]) != "" || truth(body["clearCredentials"])})
			result = storageView(c)
		})
		return result
	})
	a.route(m, "POST /api/admin/storage/test", 3, func(r *Request) any {
		c := a.parseStorage(a.Store, r.Body())
		if err := validateOSS(c, true); err != nil {
			fail(400, err.Error())
		}
		ctx, cancel := context.WithTimeout(r.R.Context(), 45*time.Second)
		defer cancel()
		key := path.Join(c.Prefix, "checks", id()+".png")
		data, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=")
		client := a.ossClient(c, false, false)
		_, err := client.PutObject(ctx, &oss.PutObjectRequest{Bucket: oss.Ptr(c.Bucket), Key: oss.Ptr(key), ContentType: oss.Ptr("image/png"), Body: strings.NewReader(string(data))})
		if err != nil {
			fail(502, ossFailure("测试上传", err).Error())
		}
		cleaned := false
		defer func() {
			if cleaned {
				return
			}
			cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			_, _ = client.DeleteObject(cleanup, &oss.DeleteObjectRequest{Bucket: oss.Ptr(c.Bucket), Key: oss.Ptr(key)})
		}()
		httpClient := a.OSSHTTP
		if httpClient == nil {
			httpClient = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		}
		seen := map[string]bool{}
		for _, useCDN := range []bool{true, false} {
			address, e := a.ossURL(c, key, useCDN)
			if e != nil {
				fail(502, e.Error())
			}
			if seen[address] {
				continue
			}
			seen[address] = true
			request, e := http.NewRequestWithContext(ctx, "GET", address, nil)
			must(e)
			response, e := httpClient.Do(request)
			if e != nil {
				fail(502, "OSS 上传成功，但读取链接验证失败，请检查 CDN 或读取域名")
			}
			body, e := io.ReadAll(io.LimitReader(response.Body, 1024))
			response.Body.Close()
			if e != nil || response.StatusCode != 200 || string(body) != string(data) {
				fail(502, "OSS 上传成功，但 CDN 或上游取图验证失败，请检查 Bucket 读权限和 CDN 配置")
			}
		}
		if _, err = client.DeleteObject(ctx, &oss.DeleteObjectRequest{Bucket: oss.Ptr(c.Bucket), Key: oss.Ptr(key)}); err != nil {
			fail(502, "OSS 上传和读取正常，但测试文件清理失败，请检查删除权限")
		}
		cleaned = true
		return Row{"ok": true, "message": "OSS 上传、读取与清理验证通过"}
	})
}
