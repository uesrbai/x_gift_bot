# API 接口

这部分 API 是给自己的管理后台、脚本或其他业务系统调用的。

> **认证方式**
>
> `/api/v1/*` 接口全部使用管理员 HTTP Basic Auth。
> 用户名固定为 `admin`，密码就是网站后台管理员密码。
>
> 不要把管理员密码写进前端代码、公开仓库或日志。建议只在服务器端调用。

## 1. 查询 X 账号是否可以接收 Premium

`POST /api/v1/account/check`

请求：

```json
{
  "username": "example"
}
```

成功：

```json
{
  "eligible": true,
  "message": "该账号当前可以接收赠送。"
}
```

如果账号当前不符合条件，接口仍返回 HTTP 200，但 `eligible` 为 `false`。

这个接口只做资格检查，不会扣款，也不会消耗兑换码。

---

## 2. 兑换卡密

`POST /api/v1/redeem`

请求：

```json
{
  "code": "XG-XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
  "username": "example"
}
```

正常开始处理时返回 HTTP 202：

```json
{
  "status": "processing",
  "progress": 20,
  "months": 6,
  "message": "正在处理，请保留本页并等待结果。"
}
```

这个接口可能触发真实的 X Premium 付款，因此调用方不要因为网络超时就立即重复提交。

如果接口返回 `processing`、`review` 或结果不明确，应该调用下面的状态接口查询原订单，而不是重新兑换。

---

## 3. 查询兑换状态

`POST /api/v1/redeem/status`

请求：

```json
{
  "code": "XG-XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
  "username": "example"
}
```

返回示例：

```json
{
  "status": "succeeded",
  "months": 6,
  "message": "已完成 Premium 赠送。",
  "progress": 100,
  "rechecking": false,
  "payment_declined": false
}
```

常见状态：

| 状态 | 含义 |
|---|---|
| `active` | 尚未兑换 |
| `processing` | 正在处理 |
| `succeeded` | 已完成 |
| `review` | 需要查询/管理员核实 |
| `revoked` | 已停用 |

---

## 4. 生成兑换码

`POST /api/v1/codes`

请求：

```json
{
  "months": 6,
  "count": 10,
  "batch": "2026-10-赠礼",
  "folder": "文件夹ID，可选"
}
```

参数：

- `months`：目前支持 `3` 或 `6`
- `count`：1–2000
- `batch`：批次名称，可选；留空会自动生成
- `folder`：已有文件夹 ID，可选

返回：

```json
{
  "codes": [
    "XG-..."
  ],
  "batch": "2026-10-赠礼",
  "months": 6,
  "folder": "..."
}
```

**生成的卡密会立即写入站点数据库，同时把完整卡密加密保存到 Vault。** 因此不需要再调用一个额外的“入库”接口。

如果你要把已经生成的卡密放进某个文件夹，使用“移动卡密”接口。

---

## 5. 停用兑换码

`POST /api/v1/codes/revoke`

请求：

```json
{
  "id": "兑换码记录ID"
}
```

只有还没有被使用的 `active` 卡密可以停用。

停用后不会再允许用户兑换。

目前不提供直接物理删除兑换码的 API。这样做是故意的：订单、兑换记录和后台审计需要保留，避免删除后无法判断某张卡密曾经发生过什么。

---

## 6. 查询兑换码列表

`GET /api/v1/codes?page=0&folder=文件夹ID`

也可以使用：

```
GET /api/v1/codes?page=0&folder=unfiled
```

返回内容包括：

- 卡密尾号
- 批次
- 套餐时长
- 状态
- 绑定的 X 用户名
- 处理进度
- 创建/更新时间
- 文件夹
- 各状态数量统计

接口不会默认返回完整卡密。

如果确实需要取回一张完整卡密，使用：

`POST /api/v1/codes/copy`

请求：

```json
{
  "id": "兑换码记录ID"
}
```

这个接口只返回该记录自己的完整卡密，并且要求管理员认证。

---

## 7. 把卡密“入库”到文件夹

如果你说的“入库”是把已经生成的卡密归到某个批次/文件夹，那么使用：

`POST /api/v1/codes/move`

请求：

```json
{
  "ids": [
    "兑换码记录ID1",
    "兑换码记录ID2"
  ],
  "folder": "文件夹ID"
}
```

如果 `folder` 传空字符串：

```json
{
  "ids": ["..."],
  "folder": ""
}
```

则会移动到“未分类”。

---

## 8. 创建文件夹

`POST /api/v1/folders`

请求：

```json
{
  "name": "2026年10月赠礼"
}
```

成功返回 HTTP 201：

```json
{
  "id": "32位ID",
  "name": "2026年10月赠礼",
  "count": 0
}
```

同名文件夹不会重复创建，会返回 HTTP 409。

### 重命名

`POST /api/v1/folders/rename`

```json
{
  "id": "文件夹ID",
  "name": "新的名称"
}
```

### 删除

`POST /api/v1/folders/delete`

```json
{
  "id": "文件夹ID"
}
```

删除文件夹不会删除卡密，里面的卡密会自动回到“未分类”。

---

## 9. 一个简单的自动化流程

例如你的其他系统要自动发放 6 个月 Premium，可以按这个顺序：

1. `POST /api/v1/account/check` 检查 X 用户名。
2. 如果 `eligible=true`，再调用 `POST /api/v1/codes` 生成卡密。
3. 记录返回的卡密 ID/批次。
4. 把卡密发给用户。
5. 用户兑换时调用 `POST /api/v1/redeem`，或者让用户直接使用网站。
6. 使用 `POST /api/v1/redeem/status` 查询最终结果。
7. 如果需要整理库存，用 `/api/v1/folders` + `/api/v1/codes/move` 管理批次。

**重要：** 兑换接口可能触发真实付款。自动化系统必须把“请求超时”和“付款失败”区分开；超时情况下优先查询原订单，不要立即重新兑换，以免造成重复操作。

## 10. curl 示例

```sh
BASE="https://xp.example.com"
AUTH="admin:你的管理员密码"

curl -u "$AUTH" \
  -H 'Content-Type: application/json' \
  -d '{"username":"example"}' \
  "$BASE/api/v1/account/check"

curl -u "$AUTH" \
  -H 'Content-Type: application/json' \
  -d '{"months":6,"count":10,"batch":"2026-10-赠礼"}' \
  "$BASE/api/v1/codes"

curl -u "$AUTH" \
  -H 'Content-Type: application/json' \
  -d '{"code":"XG-...","username":"example"}' \
  "$BASE/api/v1/redeem"

curl -u "$AUTH" \
  -H 'Content-Type: application/json' \
  -d '{"code":"XG-...","username":"example"}' \
  "$BASE/api/v1/redeem/status"
```

## 11. 与网页接口的关系

`/api/v1/*` 不是另一套业务逻辑，而是稳定的自动化入口。

它复用现有网页后台的校验、数据库、Vault、付款锁和兑换流程。这样以后网页界面调整时，API 的用途不会跟着变化。

公开用户端仍然可以使用原来的：

- `POST /api/check`
- `POST /api/redeem`
- `POST /api/status`

自动化系统建议使用 `/api/v1/*`，因为这些接口明确要求管理员认证。

## 12. 支付节点与支付卡管理

下面这些接口是管理员接口，使用与 `/api/v1/*` 相同的 HTTP Basic Auth（用户名固定为 `admin`）。

### 12.1 查询支付节点

`GET /api/admin/payment/nodes`

返回节点数量、可用数量、冷却数量，以及每个节点的节点 ID（脱敏）、协议类型和最近探测到的出口 IP。

**不会返回节点密码、UUID、密钥或完整 outbound JSON。**

### 12.2 设置支付节点池

`PUT /api/admin/payment/nodes`

请求体直接使用支付节点 JSON 数组：

```json
[
  {
    "type": "anytls",
    "server": "example.com",
    "server_port": 443,
    "password": "..."
  }
]
```

项目会先通过 `proxy.ParseOutboundPool` 校验，再加密写入 Vault 的 `payment-outbounds`。

传入空数组可以恢复 **direct** 模式。最多 128 个节点，单次请求最大 1 MiB。

### 12.3 探测支付节点

`POST /api/admin/payment/nodes/probe`

系统会通过每个节点访问 Stripe 公网入口和 IP 查询服务，返回节点 ID、协议类型、Stripe HTTP 状态、出口 IP、健康状态和非敏感错误原因。

探测结果中的出口 IP 会加密保存，供后台后续查看。

### 12.4 查询支付卡状态

`GET /api/admin/payment/cards`

只返回脱敏信息，例如卡号后四位、是否可用、是否冷却/阻断、阻断原因、卡+节点组合冷却数量和当前轮换使用情况。

**不会返回完整卡号或 CVC。**

### 12.5 新增支付卡

`POST /api/admin/payment/cards`

请求体可以是单张卡对象，也可以是卡数组：

```json
{
  "number": "...",
  "month": "10",
  "year": "2028",
  "cvc": "...",
  "name": "Card Holder",
  "email": "billing@example.com",
  "country": "US",
  "postal": "90000",
  "line1": "Example Street 1",
  "line2": "",
  "city": "Los Angeles",
  "state": "CA"
}
```

新增/更新前会执行卡号 Luhn、有效期、CVC、账单国家等校验。

### 12.6 替换整个支付卡集合

`PUT /api/admin/payment/cards`

请求体必须是完整 JSON 数组，适合批量维护卡池；生产环境不要把请求体写入日志。

### 12.7 删除一张支付卡

`POST /api/admin/payment/cards/remove`

```json
{
  "last4": "1234"
}
```

只允许通过后四位删除，并且不会允许删除最后一张支付卡。

### 12.8 更新统一账单信息

`POST /api/admin/payment/cards/billing`

可更新 `billing_name`、`email`、`billing_country`、`billing_postal_code`、`billing_address_line1`、`billing_address_line2`、`billing_city`、`billing_state`。

更新后会重新验证所有已配置支付卡。

### 12.9 清除支付卡阻断/冷却

`POST /api/admin/payment/cards/unblock`

这是明确的管理员操作，用于清除持久化的卡片阻断状态。

**注意：** 付款被 Stripe 拒绝后，不建议无条件立即解锁并重复付款。先确认拒绝原因，再决定是否恢复卡片。

### 12.10 安全说明

支付节点和支付卡都存储在加密 Vault 中。管理接口本身只应该通过 HTTPS 和管理员认证访问。

不要把真实卡号、CVC、节点密码、UUID 或完整 outbound JSON 放进 Git、README、Issue、日志或聊天记录。
