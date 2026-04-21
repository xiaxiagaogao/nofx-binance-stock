# Session-Aware Scan Interval — 技术说明文档

**状态**: 已实装，存在待排查 bug（开市时段实测仍为 1h）  
**相关 commits**: `1a49ecb` → `0225e73`  
**涉及文件**: `store/strategy.go` · `trader/auto_trader.go` · `kernel/engine.go`

---

## 一、需求背景

原系统使用固定 `time.NewTicker`，扫描间隔由用户在 UI「AI 扫描决策间隔」字段手动设置（当前为 20min），全天候统一频率。

**问题**：美股正式开盘约 6.5 小时，流动性充足，20min 合理；但休市/周末长达 16+ 小时，同频率扫描既无意义又消耗 API 配额。

**目标**：根据当前美股交易时段，动态调整每次扫描后的等待时长。

---

## 二、设计方案

### 时段划分与目标间隔

| 时段 key | 间隔 | ET 时间 | HKT（UTC+8）|
|---|---|---|---|
| `us_market_open` | **20 min** | 09:30–16:00 | 21:30–04:00 |
| `us_pre_market` | **60 min** | 04:00–09:30 | 16:00–21:30 |
| `us_after_hours` | **60 min** | 16:00–20:00 | 04:00–08:00 |
| `us_market_closed` | **120 min** | 20:00–04:00 + 周末 | 08:00–16:00 + 周末 |

### 优先级链

```
策略 DB 中的 session_scan_intervals（可自定义覆盖）
        ↓ 若 nil（旧策略配置兼容）
builtinSessionIntervals（代码硬编码，始终生效）
        ↓ 若 session key 缺失（理论上不会发生）
UI 设置的扫描间隔（当前 20min，作为最终 fallback）
```

Grid 策略**跳过**上述逻辑，始终使用固定 `ScanInterval`。

---

## 三、实现细节

### 3.1 `kernel/engine.go` — 时段判断

**函数**: `GetUSTradingSession(utcNow time.Time) string`

```go
func GetUSTradingSession(utcNow time.Time) string {
    loc, err := time.LoadLocation("America/New_York")
    if err != nil {
        loc = time.FixedZone("EDT", -4*3600) // fallback UTC-4
    }
    et := utcNow.In(loc)

    // 周末直接返回 closed
    if et.Weekday() == time.Saturday || et.Weekday() == time.Sunday {
        return "us_market_closed"
    }

    totalMinutes := et.Hour()*60 + et.Minute()
    switch {
    case totalMinutes >= 4*60 && totalMinutes < 9*60+30:   return "us_pre_market"
    case totalMinutes >= 9*60+30 && totalMinutes < 16*60:  return "us_market_open"
    case totalMinutes >= 16*60 && totalMinutes < 20*60:    return "us_after_hours"
    default:                                                return "us_market_closed"
    }
}
```

**注意**：使用 `time.LoadLocation("America/New_York")` 自动处理夏/冬令时（EDT/EST），无需手动维护偏移量。

---

### 3.2 `store/strategy.go` — 间隔配置与读取

**新增字段**（`RiskControlConfig` struct）:
```go
SessionScanIntervals map[string]int `json:"session_scan_intervals,omitempty"`
```

**硬编码 fallback**（commit `0225e73` 新增，解决旧策略配置兼容问题）:
```go
var builtinSessionIntervals = map[string]int{
    "us_market_open":   20,
    "us_pre_market":    60,
    "us_after_hours":   60,
    "us_market_closed": 120,
}
```

**读取方法**:
```go
func (r RiskControlConfig) GetSessionScanInterval(
    session string,
    defaultInterval time.Duration,
) time.Duration {
    m := r.SessionScanIntervals
    if m == nil {
        m = builtinSessionIntervals // 旧配置自动走 builtin
    }
    if minutes, ok := m[session]; ok && minutes > 0 {
        return time.Duration(minutes) * time.Minute
    }
    return defaultInterval // 最终 fallback
}
```

**默认配置**（`GetDefaultStrategyConfig()` 中）:
```go
SessionScanIntervals: map[string]int{
    "us_market_open":   20,
    "us_pre_market":    60,
    "us_after_hours":   60,
    "us_market_closed": 120,
},
```

---

### 3.3 `trader/auto_trader.go` — 主循环改造

**原实现**（已移除）:
```go
ticker := time.NewTicker(at.config.ScanInterval) // 固定间隔，全天不变
defer ticker.Stop()
...
case <-ticker.C:
    runCycle()
```

**新实现**（commit `1a49ecb` + `0225e73`）:
```go
for {
    // ... 检查 isRunning ...

    // 每次循环结束后，根据当前时段计算下次等待时长
    sleepDur := at.config.ScanInterval
    if !isGridStrategy {
        session := kernel.GetUSTradingSession(time.Now().UTC())
        var riskCtrl store.RiskControlConfig
        if at.config.StrategyConfig != nil {
            riskCtrl = at.config.StrategyConfig.RiskControl
        }
        sleepDur = riskCtrl.GetSessionScanInterval(session, at.config.ScanInterval)
        if sleepDur != at.config.ScanInterval {
            logger.Infof("⏱️  [%s] Session '%s' → next scan in %v",
                at.name, session, sleepDur)
        }
    }

    select {
    case <-time.After(sleepDur):
        runCycle()
    case <-at.stopMonitorCh:
        return nil
    }
}
```

**关键设计点**：
- `time.NewTicker` → `time.After`：每次动态计算，而非固定周期
- `StrategyConfig` 为 nil 时用空 `RiskControlConfig{}` → 自动走 `builtinSessionIntervals`
- 时段变化日志仅在间隔**不等于** UI 设置值时打印，避免日志噪音
- Grid 策略完全跳过，行为不变

---

## 四、修改历史

| Commit | 时间 | 内容 |
|---|---|---|
| `1a49ecb` | 2026-04-16 | 首次实装：移除 Ticker，加 SessionScanIntervals 字段和 GetSessionScanInterval()，strategy-45 备份中写入默认值 |
| `0225e73` | 2026-04-18 | Bug fix：旧策略配置 SessionScanIntervals 为 nil 时 fallback 到 UI 值而非预期间隔，新增 builtinSessionIntervals 硬编码保底，移除 StrategyConfig nil guard |

---

## 五、已知问题

**现象**：用户反馈开市时段实测仍为 1h 间隔，未按预期走 20min。

**尚未排查的可能原因**：

1. **EC2 未重启 trader**  
   代码已 push 但 trader 进程仍在内存中运行旧逻辑，需 stop → start 使新代码生效

2. **`time.LoadLocation` 时区文件缺失**  
   EC2 Linux 系统可能未安装 `tzdata`，导致 `LoadLocation("America/New_York")` 失败，fallback 到硬编码 UTC-4（EDT），在冬令时（UTC-5）期间时段判断偏差 1 小时

3. **策略配置未正确加载**  
   `at.config.StrategyConfig` 加载时序问题，导致每次进入循环时 `StrategyConfig` 仍为旧对象

4. **日志未启用**  
   日志条件为 `sleepDur != at.config.ScanInterval`，若判断结果恰好与 UI 值相同（例如 UI=60min，session=pre_market 也是 60min），日志不会打印，难以确认逻辑是否执行

**排查建议**：
```bash
# 1. 确认 trader 已用新代码重启
# 2. 在 EC2 上确认时区文件存在
ls /usr/share/zoneinfo/America/New_York

# 3. 在 server log 中搜索 session 日志
grep "Session '" /path/to/trader.log | tail -20

# 4. 临时加强日志（排查期间）：
# 把 auto_trader.go 中的日志条件改为无条件打印
logger.Infof("⏱️  [%s] Session '%s' → sleep %v", at.name, session, sleepDur)
```

---

*文档生成于 2026-04-20，对应 main 分支最新状态。*
