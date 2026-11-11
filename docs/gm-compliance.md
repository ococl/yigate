# 国密（TLCP）改造自查清单

用途：密评、等保测评或甲方验收前的**自查**。每条给出实现位置、配置项与可执行的验证命令，
便于现场取证留档。

两点前提，先说清楚：

- 本清单**不构成测评结论**。是否满足 GB/T 39786 的某一级要求，取决于业务整体密码应用方案，
  不是单个组件能自证的。
- YiGate 只负责**传输层**国密。应用层签名验签、密钥管理系统、证书签发服务都不在其职责内。

## 1. 实施范围

**已覆盖**

| 位置 | 能力 |
|---|---|
| 入站监听 | TLCP（GB/T 38636-2020），SM2 双证书；`auto` 同端口自适应 TLCP / 标准 TLS |
| 出站 https 上游 | `tls_protocol: gm` 强制国密；`auto` 国密优先并记忆协商结果 |
| 健康检查 | `https`/`tls` 探测复用数据面协商器，国密上游不会被误判为不可用 |

**不覆盖（需业务侧补齐）**

- 管理面（WebUI / 管理 API）：跟随浏览器能力走标准 TLS/HTTP，不提供 TLCP。
  如管理面也必须国密，应在前置国密网关处终结。
- DNS over TLS / HTTPS：公共 DNS 服务不使用国密套件，改造点在服务端。
- 应用层密码应用：数据签名、验签、电子签章、数据库字段加密等。
- 证书生命周期：`gencert` 只生成**样板**证书，不是 CA 服务。

## 2. 逐项自查表

### 2.1 传输通道机密性与完整性

| 关注点 | 实现现状 | 配置项 | 如何验证 |
|---|---|---|---|
| 协议 | TLCP，握手与记录层由 `github.com/Trisia/gotlcp` 实现 | `tls.protocol: gm` | 启动日志 `"protocol":"gm"` |
| 强制国密 | `gm` 模式握手失败即失败，**不做任何降级** | `tls.protocol: gm` | 见 2.4「强制与兼容」 |
| 密码套件 | 白名单 4 个，默认 GCM 优先 | `tls.cipher_suites` | 配非法名启动即报错 |
| 摘要 | SM3（随套件协商） | — | 由握手结果决定 |
| 会话复用 | LRU 会话缓存，容量 128 | 内置常量 | — |

套件白名单（`cipher_suites` 留空即用默认优先级）：

```
ECDHE_SM4_GCM_SM3   ← 默认优先
ECC_SM4_GCM_SM3
ECDHE_SM4_CBC_SM3
ECC_SM4_CBC_SM3
```

写成未知名称会在**加载配置阶段**直接失败，不会静默忽略：

```bash
$ ./yigate -config bad.yml run
load config: tls: unsupported tls.cipher_suites entry "SM4_GCM" \
  (want one of ECDHE_SM4_GCM_SM3, ECC_SM4_GCM_SM3, ECDHE_SM4_CBC_SM3, ECC_SM4_CBC_SM3)
```

### 2.2 通信双方身份鉴别

| 关注点 | 实现现状 | 配置项 | 备注 |
|---|---|---|---|
| 服务端双证书 | SM2 签名证书 + 加密证书，两套私钥分别加载 | `gm_sign_cert_file` / `gm_sign_key_file`、`gm_enc_cert_file` / `gm_enc_key_file` | 四者必须配套，缺一即启动失败 |
| 私钥与证书配对 | 加载时比对公钥坐标，不匹配直接报错 | — | `verifySM2KeyPair` |
| 客户端鉴别（mTLS） | `client_ca_file` 非空即 `RequireAndVerifyClientCert` | `tls.client_ca_file` | TLCP 侧用 `smx509` 证书池 |
| 证书用途（KeyUsage） | **不校验**签名/加密证书的 KeyUsage 字段 | — | 见第 5 节边界 |
| 证书吊销 | **不支持** CRL / OCSP | — | 见第 5 节边界 |

配置完整性校验（`protocol: gm` 但缺双证书时启动即失败）：

```bash
$ ./yigate -config bad.yml run
load config: tls: tls.protocol gm requires tls.gm_sign_cert_file/gm_sign_key_file \
  and tls.gm_enc_cert_file/gm_enc_key_file
```

### 2.3 密钥与证书管理

| 关注点 | 实现现状 | 配置项 / 命令 |
|---|---|---|
| 私钥落盘加密 | `gencert -p <pwd>` 输出 PBES2-SM4 加密的 PKCS#8 | `./yigate gencert -o certs/gm -d gw.example.com -p '...'` |
| 口令传递 | 支持 `${ENV}` 引用环境变量，运行时展开，配置不落明文 | `gm_sign_key_password` / `gm_enc_key_password` |
| 有效期 | 叶子默认 365 天，`-days` 可调；不超过签发 CA 的到期时间 | `-days` |
| 过期 CA | 拒绝签发，避免产出当场失效的链 | — |
| 到期监控 | 启动与每次热加载扫描；30 天内告警、已过期报错 | 日志 + `/api/v1/status` + WebUI 横幅 |
| 离线体检 | 文件或目录，过期/临期退出码 1 | `gencert -check` |

现场取证（三份证据一起留存，覆盖"配置—证书—运行时"）：

```bash
# 1) 证书有效期与剩余天数（过期或 30 天内 → exit 1）
./yigate gencert -check certs/gm
echo "exit=$?"

# 2) 运行时证书状态（与 -check 同源，可接监控）
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8890/api/v1/status \
  | python3 -c 'import json,sys; print(json.dumps(json.load(sys.stdin).get("certificates", []), ensure_ascii=False, indent=1))'

# 3) 私钥是否加密落盘：PEM 头两种都是 PRIVATE KEY，看不出区别，要看 ASN.1 结构
openssl asn1parse -in certs/gm/sign.key -inform PEM | head -3
#   加密：5:d=2 ... OBJECT :1.2.156.10197.6.4.1.5.2   ← PBES2-SM4 算法标识
#   明文：3:d=1 ... INTEGER :00                       ← 直接是 PKCS#8 版本号
```

`-check` 输出示例：

```
FILE               KIND  SUBJECT         NOT BEFORE  NOT AFTER   REMAINING  STATUS
certs/gm/ca.crt    ca    yigate GM CA    2026-11-11  2036-11-08  3649 days  ok
certs/gm/enc.crt   leaf  gw.example.com  2026-11-11  2026-12-01  19 days    expiring
```

**口令无法原地更换**（测评常被追问）：轮换叶子私钥口令 = 用相同 `-ca` 重新签发一套（`-p` 新口令）
+ 更新环境变量 + 滚动重启。CA 私钥口令目前不支持重封装，更换需重建 CA 并同步替换所有下游信任锚。

### 2.4 强制国密与兼容模式（最容易踩的一条）

`auto` 是**兼容模式**，不是国密模式：

| 配置 | 行为 | 适用场景 |
|---|---|---|
| `protocol: gm` | 只接受 TLCP，握手不成功即失败，**无降级路径** | 要求"必须国密"的验收口径 |
| `protocol: tls` | 只走标准 TLS（改造前行为） | 存量服务 |
| `protocol: auto` | 国密优先，失败回落标准 TLS，并记忆协商结果 | 过渡期、混合客户端 |

同一逻辑适用于出站 `tls_protocol`。**如果验收要求"通信双方必须使用国密算法"，就必须配置 `gm`
并把 `auto` 从配置里清掉**——`auto` 端口上标准 TLS 客户端仍然能连上来，测评时会被直接判为
"未强制使用密码算法"。

核对命令：

```bash
grep -n "protocol" config.yml listeners/*.yml 2>/dev/null
# 期望：所有对外端口为 gm；出现 auto 需能说明业务理由
```

## 3. 现场取证清单

按顺序执行，输出直接截图留档：

| 步骤 | 命令 | 期望 |
|---|---|---|
| 版本 | `./yigate -version` | 版本、commit、构建时间 |
| 证书体检 | `./yigate gencert -check certs/gm` | 全部 `ok`；临期项可见 |
| 配置校验 | `./yigate -config config.yml run` | 配置非法时在加载阶段退出，不进入服务循环 |
| 启动日志 | 同上 | `"tls":true,"protocol":"gm"`，无到期告警 |
| 状态接口 | `GET /api/v1/status` | `certificates` 含全部引用证书 |
| 性能复测 | `./scripts/bench-tls.sh BenchmarkHandshake 5s` | 与本机基线比对，勿引用仓库参考值 |

启动日志中的国密标志：

```
{"msg":"yigate listening","address":"0.0.0.0:8443","tls":true,"protocol":"gm","tproxy":false}
```

## 4. 与测评项的常见对应

| 测评关注点 | 本程序对应实现 | 说明 |
|---|---|---|
| 通信数据机密性 | TLCP + SM4-GCM | 套件由 `cipher_suites` 白名单约束 |
| 通信数据完整性 | SM4-GCM 认证标签 / SM3 | 随套件协商 |
| 通信实体身份鉴别 | SM2 双证书；可选 mTLS | 服务端证书 + `client_ca_file` |
| 密码算法合规 | SM2 / SM3 / SM4 | 无国际算法回退（`gm` 模式下） |
| 密钥管理 | 私钥 PBES2-SM4 加密、口令走环境变量 | 不含 KMS，密钥托管需业务侧方案 |
| 证书有效性 | 到期扫描 + 离线体检 | 不含吊销检查 |

## 5. 已知边界（如实说明，避免测评现场被动）

1. **不校验 KeyUsage**：签名证书与加密证书由 TLCP 库按握手角色使用，本程序不做
   `digitalSignature` / `keyEncipherment` 的字段校验。用 `gencert` 生成的证书天然正确。
2. **无 CRL / OCSP**：证书吊销依赖人工轮换与重启，程序内不做在线吊销查询。
3. **无密钥管理系统**：私钥以文件形式存在，加密与权限靠文件系统与 PBES2-SM4 保障。
4. **管理面不国密**：WebUI 与管理 API 使用标准 TLS/HTTP。
5. **`auto` 允许降级**：见 2.4，验收要求"强制国密"时必须用 `gm`。
6. **SM3 的用法有限**：仅用于管理令牌比较前的定长化（配合常量时间比较），
   不承担存储或完整性职责，换成 SM3 属于口径统一，不构成额外安全增益。
7. **TLCP 握手更快不等于更安全**：TLCP 基于 TLS 1.1 风格的握手，消息更少；
   性能对照见 README「性能基线」，不要把它当成安全强度结论。

## 6. 如需更进一步

按投入产出排序，这几项能把边界收窄：

- `gencert -check` 增加链验证（叶子是否真由给定 CA 签发）——目前只报到期。
- 加载时校验双证书 KeyUsage——挡住"两套证书拿反了"的配置错误。
- 证书指纹登记与变更告警——替代 CRL 的轻量做法。

相关文档：README「国密（TLCP）适配」、`SECURITY.md`。
