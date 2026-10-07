package site

import (
 "bytes"
 "context"
 "crypto/rand"
 "crypto/sha256"
 "crypto/subtle"
 "encoding/base64"
 "encoding/json"
 "errors"
 "fmt"
 "log"
 "net"
 "net/http"
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "time"
 "xgift/internal/checkout"
 "xgift/internal/proxy"
 "xgift/internal/vault"
)

const (
 setupTokenFile="setup-token"
 defaultSetupBearer="Bearer AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs%3D1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA"
 defaultSetupUserAgent="Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
 defaultSetupMerchant="acct_1Ika5JA3KZ32dPo1"
 defaultSetupCurrency="bdt"
 defaultSetupProduct3="prod_TJXJtpzqCpI36N"
 defaultSetupProduct6="prod_TJXKKNJwZJIhCM"
)
var setupStripePattern=regexp.MustCompile("^pk_live_[A-Za-z0-9]+$")
var setupCardNumberPattern=regexp.MustCompile("^[0-9]{12,19}$")
var setupMonthPattern=regexp.MustCompile("^(0[1-9]|1[0-2])$")
var setupYearPattern=regexp.MustCompile("^[0-9]{4}$")
var setupCVCPattern=regexp.MustCompile("^[0-9]{3,4}$")
var setupCountryPattern=regexp.MustCompile("^[A-Z]{2}$")

type setupRequest struct {
 Token string `json:"token"`; AdminPassword string `json:"admin_password"`; AuthToken string `json:"auth_token"`; CT0 string `json:"ct0"`
 Authorization string `json:"authorization"`; UserAgent string `json:"user_agent"`; StripeKey string `json:"stripe_key"`
 CardNumber string `json:"card_number"`; CardMonth string `json:"card_month"`; CardYear string `json:"card_year"`; CardCVC string `json:"card_cvc"`
 BillingName string `json:"billing_name"`; BillingEmail string `json:"billing_email"`; BillingCountry string `json:"billing_country"`
 BillingPostal string `json:"billing_postal"`; BillingLine1 string `json:"billing_line1"`; BillingLine2 string `json:"billing_line2"`
 BillingCity string `json:"billing_city"`; BillingState string `json:"billing_state"`; ProxyJSON string `json:"proxy_json"`; EnablePayments bool `json:"enable_payments"`
}

func setupToken(path string)(string,error){if b,e:=os.ReadFile(path);e==nil{return strings.TrimSpace(string(b)),nil}else if !errors.Is(e,os.ErrNotExist){return "",e};b:=make([]byte,24);if _,e:=rand.Read(b);e!=nil{return "",e};t:=base64.RawURLEncoding.EncodeToString(b);if e:=os.WriteFile(path,[]byte(t+"\n"),0600);e!=nil{return "",e};fmt.Println("============================================================");fmt.Println("XGift 首次网页初始化令牌（只显示一次，请勿分享）：");fmt.Println(t);fmt.Println("打开站点首页完成初始化。");fmt.Println("============================================================");return t,nil}
func setupTokenValid(path,s string)bool{b,e:=os.ReadFile(path);if e!=nil{return false};a:=sha256.Sum256([]byte(strings.TrimSpace(string(b))));c:=sha256.Sum256([]byte(strings.TrimSpace(s)));return subtle.ConstantTimeCompare(a[:],c[:])==1}
func setupReady(dir string)bool{_,e:=os.Stat(filepath.Join(dir,"vault.db"));return e==nil}

func runFirstSetup(ctx context.Context,dir,passwordFile,adminPasswordFile,origin,listen string)error{
 tp:=filepath.Join(dir,setupTokenFile);if _,e:=setupToken(tp);e!=nil{return e};m:=http.NewServeMux()
 m.HandleFunc("GET /{$}",func(w http.ResponseWriter,r *http.Request){setupAsset(w,r,"setup.html","text/html; charset=utf-8")})
 m.HandleFunc("GET /setup",func(w http.ResponseWriter,r *http.Request){setupAsset(w,r,"setup.html","text/html; charset=utf-8")})
 m.HandleFunc("GET /healthz",func(w http.ResponseWriter,r *http.Request){reply(w,200,map[string]any{"ok":true,"setup_required":true})})
 m.HandleFunc("GET /api/setup/status",func(w http.ResponseWriter,r *http.Request){reply(w,200,map[string]any{"setup_required":true})})
 m.HandleFunc("POST /api/setup",func(w http.ResponseWriter,r *http.Request){handleFirstSetup(w,r,dir,passwordFile,adminPasswordFile,tp,origin)})
 addr:=listen;if addr==""{addr="127.0.0.1:8787"};h:=&http.Server{Addr:addr,Handler:setupMiddleware(m),ReadHeaderTimeout:5*time.Second,ReadTimeout:20*time.Second,WriteTimeout:60*time.Second,IdleTimeout:60*time.Second,MaxHeaderBytes:128<<10};done:=make(chan error,1);go func(){done<-h.ListenAndServe()}();log.Printf("xgift-web first-run setup listening on %s",addr)
 select{case e:=<-done:return e;case <-ctx.Done():s,c:=context.WithTimeout(context.Background(),5*time.Second);defer c();return h.Shutdown(s)}
}
func setupMiddleware(n http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Header().Set("Cache-Control","no-store");w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("X-Frame-Options","DENY");w.Header().Set("Referrer-Policy","no-referrer");n.ServeHTTP(w,r)})}
func setupAsset(w http.ResponseWriter,r *http.Request,name,kind string){b,e:=assets.ReadFile("assets/"+name);if e!=nil{http.NotFound(w,r);return};nonce:=token(16);b=bytes.ReplaceAll(b,[]byte("__XGIFT_NONCE__"),[]byte(nonce));w.Header().Set("Content-Security-Policy","default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'");w.Header().Set("Content-Type",kind);_,_=w.Write(b)}

func handleFirstSetup(w http.ResponseWriter,r *http.Request,dir,passwordFile,adminPasswordFile,tokenPath,configuredOrigin string){
 if !setupTokenValid(tokenPath,r.Header.Get("X-XGift-Setup-Token")){message(w,401,"初始化令牌无效或已过期。");return}
 if configuredOrigin!=""&&r.Header.Get("Origin")!=""&&r.Header.Get("Origin")!=configuredOrigin{message(w,403,"请求来源与站点域名不一致。");return}
 var q setupRequest;if !decodeSetupJSON(w,r,&q){return};if e:=validateSetupRequest(&q);e!=nil{message(w,400,e.Error());return};if setupReady(dir){message(w,409,"实例已经完成初始化。");return}
 raw:=[]byte(strings.TrimSpace(q.ProxyJSON));ctx,c:=context.WithTimeout(r.Context(),15*time.Second);defer c();p,e:=freeLoopbackPort();if e!=nil{message(w,500,"无法准备代理测试端口。");return};box,e:=proxy.Start(ctx,raw,p);if e!=nil{message(w,400,"代理配置无效："+e.Error());return};_=box.Close()
 if _,e=os.Stat(passwordFile);e!=nil{message(w,500,"Vault 密码文件不存在，请重新部署服务。");return};if e=os.Chmod(passwordFile,0600);e!=nil{message(w,500,"无法保护 Vault 密码文件。");return}
 vaultPath:=filepath.Join(dir,"vault.db");v,e:=vault.Open(vaultPath,passwordFile,true);if e!=nil{message(w,500,"无法创建加密 Vault："+e.Error());return};completed:=false;defer func(){v.Close();if !completed{_=os.Remove(vaultPath)}}()
 cookies,_:=json.Marshal(map[string]any{"cookies":[]map[string]string{{"name":"auth_token","value":q.AuthToken,"domain":".x.com"},{"name":"ct0","value":q.CT0,"domain":".x.com"}}});if e=v.Put("cookies",cookies);e!=nil{message(w,500,"保存 X Cookie 失败。");return}
 auth,_:=json.Marshal(map[string]string{"Authorization":q.Authorization,"UserAgent":q.UserAgent});if e=v.Put("api-auth",auth);e!=nil{message(w,500,"保存 X API 配置失败。");return};if e=v.Put("proxy",raw);e!=nil{message(w,500,"保存代理配置失败。");return};if e=v.Put("stripe-key",[]byte(q.StripeKey));e!=nil{message(w,500,"保存 Stripe 公钥失败。");return}
 cat:=checkout.Catalog{Merchant:defaultSetupMerchant,Currency:defaultSetupCurrency,Plans:[]checkout.CatalogPlan{{Months:3,Amount:30000,Product:defaultSetupProduct3},{Months:6,Amount:60000,Product:defaultSetupProduct6}}};cb,_:=json.Marshal(cat);if _,e=checkout.ParseCatalog(cb);e!=nil{message(w,500,"默认商品目录无效。");return};if e=v.Put("catalog",cb);e!=nil{message(w,500,"保存商品目录失败。");return}
 card:=map[string]string{"number":q.CardNumber,"exp_month":q.CardMonth,"exp_year":q.CardYear,"cvc":q.CardCVC,"billing_name":q.BillingName,"email":q.BillingEmail,"billing_country":q.BillingCountry,"billing_postal_code":q.BillingPostal,"billing_address_line1":q.BillingLine1,"billing_address_line2":q.BillingLine2,"billing_city":q.BillingCity,"billing_state":q.BillingState};cr,_:=json.Marshal(card);if _,e=checkout.SetCardRecords(v,cr);e!=nil{message(w,400,"支付卡配置无效："+e.Error());return}
 if q.EnablePayments{if e=checkout.CheckPaymentConfiguration(v);e!=nil{message(w,400,"无法开启支付："+e.Error());return}}
 a:=[]byte(q.AdminPassword);if e=os.WriteFile(adminPasswordFile,append(a,'\n'),0600);e!=nil{clear(a);message(w,500,"保存管理员密码失败。");return};clear(a);if e=os.WriteFile(filepath.Join(dir,"setup-complete"),[]byte(time.Now().UTC().Format(time.RFC3339)+"\n"),0600);e!=nil{message(w,500,"保存初始化状态失败。");return};completed=true;_=os.Remove(tokenPath);en:="false";if q.EnablePayments{en="true"};_=os.WriteFile(filepath.Join(dir,"payments-enabled"),[]byte(en+"\n"),0600)
 reply(w,200,map[string]any{"ok":true,"restart_required":true,"message":"初始化完成。服务将自动重启；请稍候再打开网站。"});go func(){time.Sleep(1200*time.Millisecond);os.Exit(0)}()
}
func decodeSetupJSON(w http.ResponseWriter,r *http.Request,q *setupRequest)bool{d:=json.NewDecoder(http.MaxBytesReader(w,r.Body,128<<10));if d.Decode(q)!=nil{message(w,400,"初始化数据格式不正确。");return false};return true}
func validateSetupRequest(q *setupRequest)error{
 q.AuthToken=strings.TrimSpace(q.AuthToken);q.CT0=strings.TrimSpace(q.CT0);q.Authorization=strings.TrimSpace(q.Authorization);q.UserAgent=strings.TrimSpace(q.UserAgent);q.StripeKey=strings.TrimSpace(q.StripeKey);q.CardNumber=strings.NewReplacer(" ","","-","").Replace(strings.TrimSpace(q.CardNumber));q.CardMonth=strings.TrimSpace(q.CardMonth);q.CardYear=strings.TrimSpace(q.CardYear);q.CardCVC=strings.TrimSpace(q.CardCVC);q.BillingName=strings.TrimSpace(q.BillingName);q.BillingEmail=strings.TrimSpace(q.BillingEmail);q.BillingCountry=strings.ToUpper(strings.TrimSpace(q.BillingCountry));q.ProxyJSON=strings.TrimSpace(q.ProxyJSON)
 if len(q.AdminPassword)<32{return errors.New("管理员密码至少 32 个字符")};if q.AuthToken==""||strings.ContainsAny(q.AuthToken,"\r\n;")||q.CT0==""||strings.ContainsAny(q.CT0,"\r\n;"){return errors.New("X Cookie 的 auth_token 与 ct0 不能为空")};if q.Authorization==""{q.Authorization=defaultSetupBearer};if !strings.HasPrefix(q.Authorization,"Bearer "){return errors.New("Authorization 必须以 Bearer 开头")};if q.UserAgent==""{q.UserAgent=defaultSetupUserAgent};if !setupStripePattern.MatchString(q.StripeKey){return errors.New("Stripe 公钥必须是 pk_live_ 开头的 publishable key")}
 if q.ProxyJSON==""{q.ProxyJSON="{\"outbounds\":[{\"type\":\"direct\",\"tag\":\"direct\"}]}"};var po map[string]any;if json.Unmarshal([]byte(q.ProxyJSON),&po)!=nil{return errors.New("代理配置必须是有效 JSON")};if _,ok:=po["outbounds"].([]any);!ok{return errors.New("代理配置必须包含 outbounds 数组")}
 if q.CardNumber==""{return errors.New("请填写支付卡；如暂时不开放支付，可先关闭支付开关，之后再通过 CLI 补充卡池")};if !setupCardNumberPattern.MatchString(q.CardNumber)||!luhn(q.CardNumber){return errors.New("卡号无效")};if !setupMonthPattern.MatchString(q.CardMonth)||!setupYearPattern.MatchString(q.CardYear)||!setupCVCPattern.MatchString(q.CardCVC){return errors.New("卡片有效期或 CVC 格式无效")};if q.BillingName==""||!strings.Contains(q.BillingEmail,"@")||!setupCountryPattern.MatchString(q.BillingCountry){return errors.New("持卡人姓名、账单邮箱或国家代码无效")};return nil
}
func luhn(s string)bool{sum:=0;for i,r:=range s{d:=int(r-'0');if (len(s)-i)%2==0{d*=2;if d>9{d-=9}};sum+=d};return sum%10==0}
func freeLoopbackPort()(int,error){l,e:=net.Listen("tcp","127.0.0.1:0");if e!=nil{return 0,e};defer l.Close();return l.Addr().(*net.TCPAddr).Port,nil}
