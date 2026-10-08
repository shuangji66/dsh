package main

// market_unavailable_test.go —— 插件市场「查不到 ≠ 未安装」的回归测试。
//
// 规则（见 market.go 的 applyMarketLocalResult 与 UpdateStatus.Unavailable）：
//   - 检测**失败**（dsh 服务未就绪：启动中 / 已停止 / 没选中任何版本 / 命令报错）→
//     Unavailable=true，界面显示「—」；LocalVersion 里**保留**上一次已知的版本号；
//   - 只有**确定查到**（`dsh plugin list` 成功退出）才写 LocalVersion：空 = 真没装
//     → 界面显示「未安装」；
//   - 首帧（进程刚起、还没检测过）也是 Unavailable=true：没检测过 = 不知道。
//
// 为什么钉这条：市场版本只能靠 `dsh plugin --profile web list` 查，而这条命令在 dsh 起来
// 之前必然失败。早期实现把「查不到」直接当成「localVersion 为空」，界面于是显示「未安装」
// —— 在 dsh 尚未就绪时，那是在说假话（用户会以为市场没装，去点安装）。

import (
	"path/filepath"
	"testing"
)

// applyMarketLocalResult 的四条语义（查不了 / 没装 / 装了 / 查不了时保留旧值）。
func TestApplyMarketLocalResultDistinguishesUnknownFromNotInstalled(t *testing.T) {
	t.Run("查不了：置 Unavailable，不写版本号", func(t *testing.T) {
		st := &UpdateStatus{Kind: updateKindMarket}
		applyMarketLocalResult(st, marketLocalSnapshot{Diag: "dsh plugin list 失败", Err: errDshNotReadyForTest})
		if !st.Unavailable {
			t.Fatal("检测失败时必须置 Unavailable（界面显示「—」而不是「未安装」）")
		}
		if st.LocalVersion != "" {
			t.Fatalf("检测失败时不该写版本号，实得 %q", st.LocalVersion)
		}
		if st.MarketDir == "" {
			t.Fatal("检测失败时要把原因留在 MarketDir（弹窗里解释为什么查不到）")
		}
	})

	t.Run("查到了但没装：清 Unavailable 与版本号", func(t *testing.T) {
		st := &UpdateStatus{Kind: updateKindMarket, Unavailable: true, LocalVersion: "1.66.11"}
		applyMarketLocalResult(st, marketLocalSnapshot{Diag: "`dsh plugin --profile web list` 未列出 dshmarket"})
		if st.Unavailable {
			t.Fatal("命令成功退出说明「查得到」，必须清掉 Unavailable（界面显示「未安装」）")
		}
		if st.LocalVersion != "" {
			t.Fatalf("确实没装时版本号必须为空，实得 %q", st.LocalVersion)
		}
	})

	t.Run("装了：写版本号并清 Unavailable", func(t *testing.T) {
		st := &UpdateStatus{Kind: updateKindMarket, Unavailable: true}
		applyMarketLocalResult(st, marketLocalSnapshot{Version: "1.66.11"})
		if st.Unavailable || st.LocalVersion != "1.66.11" {
			t.Fatalf("装着的版本必须写进去且不再 Unavailable，实得 %+v", st)
		}
	})

	t.Run("查不了时保留上一次已知的版本号（不清空）", func(t *testing.T) {
		st := &UpdateStatus{Kind: updateKindMarket, LocalVersion: "1.66.11", LatestVersion: "1.67.0"}
		applyMarketLocalResult(st, marketLocalSnapshot{Diag: "dsh 服务已停止", Err: errDshNotReadyForTest})
		if !st.Unavailable {
			t.Fatal("查不了时必须置 Unavailable")
		}
		if st.LocalVersion != "1.66.11" {
			t.Fatalf("已知版本不该因为一次探测失败被清掉（插件不会因 dsh 重启消失），实得 %q", st.LocalVersion)
		}
		if !st.HasUpdate {
			t.Fatal("已知 1.66.11 而最新版是 1.67.0：红点应保持亮")
		}
		if st.LatestVersion != "1.67.0" {
			t.Fatalf("LatestVersion 不能被本地检测动到，实得 %q", st.LatestVersion)
		}
	})
}

// 端到端：同一个「没装」的空版本号，在「查不了」与「查到了」两种情况下结论必须不同。
func TestRefreshMarketLocalMarksUnavailableWhenDshNotReady(t *testing.T) {
	dataDir := t.TempDir()
	// DshVersion 为空 = 本机没有选中的 dsh 版本：`dsh plugin list` 必然失败。
	upd := newMarketTestManager(t, dataDir, "")

	if snap := upd.refreshMarketLocal(true); snap.Err == nil {
		t.Fatal("没选中 dsh 版本时本地检测应当失败")
	}
	st := upd.getStatus(updateKindMarket)
	if !st.Unavailable {
		t.Fatal("dsh 未就绪时市场状态必须置 Unavailable（界面显示「—」）")
	}
	if st.LocalVersion != "" {
		t.Fatalf("dsh 未就绪时不该有版本号，实得 %q", st.LocalVersion)
	}
	if st.MarketDir == "" {
		t.Fatal("必须留下「为什么查不到」的诊断文本")
	}
}

// 对照：dsh 能查（命令成功）时，没装就是没装 —— Unavailable 必须为 false。
func TestRefreshMarketLocalClearsUnavailableWhenQuerySucceeds(t *testing.T) {
	dataDir := t.TempDir()
	upd := newMarketTestManager(t, dataDir, "0.2.0")
	fakeDshPlugin(t, dataDir, "0.2.0", samplePluginListNoMarket)

	if snap := upd.refreshMarketLocal(true); snap.Err != nil {
		t.Fatalf("假 dsh 正常退出时不该报错: %v", snap.Err)
	}
	st := upd.getStatus(updateKindMarket)
	if st.Unavailable {
		t.Fatal("查得到（命令成功、只是没列出市场）时 Unavailable 必须为 false → 界面显示「未安装」")
	}
	if st.LocalVersion != "" {
		t.Fatalf("没装市场时版本号应为空，实得 %q", st.LocalVersion)
	}
}

// 从「查不了」恢复到「查得到」：dsh 起来后重查一次就能拿到真实版本号（前端从「—」变版本号）。
func TestRefreshMarketLocalRecoversAfterDshBecomesReady(t *testing.T) {
	dataDir := t.TempDir()
	upd := newMarketTestManager(t, dataDir, "0.2.0")
	fakeDshPlugin(t, dataDir, "0.2.0", pluginListWithMarket("1.66.11"))

	// 先制造「查不了」：把选中版本清掉，探测必失败。
	st := upd.getStatus(updateKindMarket)
	st.Unavailable = true
	upd.setStatus(updateKindMarket, &st)

	// dsh 就绪后（这里直接把配置恢复成选中 0.2.0）重查：应拿到版本号并清掉 Unavailable。
	initConfig(&AppConfig{DshPort: 0, DshVersion: "0.2.0"})
	upd.refreshMarketLocal(true)

	got := upd.getStatus(updateKindMarket)
	if got.Unavailable || got.LocalVersion != "1.66.11" {
		t.Fatalf("dsh 就绪后应恢复成「装着 1.66.11」，实得 %+v", got)
	}
}

// 首帧：还没检测过就是「不知道」——界面显示「—」而不是「未安装」。
func TestMarketStatusStartsUnknown(t *testing.T) {
	dataDir := t.TempDir()
	prevCfg := GetConfig()
	initConfig(&AppConfig{DshPort: 0})
	t.Cleanup(func() { initConfig(&prevCfg) })

	renv := &RuntimeEnv{DataDir: dataDir, ConfigFile: filepath.Join(dataDir, "config.json")}
	dsh := &DshManager{renv: renv, logf: func(logLevel, string, ...interface{}) {}}
	upd := newUpdateManager(renv, dsh)

	st := upd.getStatus(updateKindMarket)
	if !st.Unavailable || st.LocalVersion != "" {
		t.Fatalf("首帧市场状态应是「不知道」（Unavailable=true、无版本号），实得 %+v", st)
	}
}

// errDshNotReadyForTest 是「查不了」用的假错误（applyMarketLocalResult 只看 Err 是否为 nil）。
var errDshNotReadyForTest = errString("dsh plugin list: not ready")

type errString string

func (e errString) Error() string { return string(e) }
