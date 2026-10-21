package main

// semver_test.go —— 迷你 semver 判定与 node-semver 的对照。
//
// 期望值全部由 node-semver 7.8.5 生成：
//
//	semver.satisfies(version, range, { includePrerelease: true })
//
// 这正是 @deepseek-ai/dsh-app-boot 判定插件兼容性时的调用方式（见其
// unmetRequirements / evaluatePluginCompatibility）。改 semver.go 后如果这里红了，
// 说明控制台的判定会与 dsh 自己不一致 —— 先确认哪边才是期望行为，别直接改期望值。

import (
	"testing"
)

// semverOracleVersions 是判定用的运行时版本集合（覆盖稳定版、预发布与跨 major）。
var semverOracleVersions = []string{
	"0.2.1-rc.2", "0.2.1", "0.2.0-rc.2", "0.2.0", "0.1.7-rc.1", "0.1.9",
	"0.3.0-0", "0.3.0", "0.2.2-alpha.1", "1.2.3", "1.2.4-beta.1", "2.0.0-0",
	"0.0.3", "0.0.4",
}

// semverOracleCases 每行是「范围 + 1/0 结果串」，1 表示 satisfies 为真。
// 其中大部分范围直接抄自真实插件 manifest（dsh-web-mobile / dshmarket / dsh-free-search…）。
var semverOracleCases = []struct {
	rng string
	row string
}{
	{">=0.2.1-rc.2", "11000011111100"},
	{"^0.2.1-rc.1", "11000000100000"},
	{">=0.1.6-alpha.2", "11111111111100"},
	{"^0.1.7-rc.1 || ^0.2.0-rc.1", "11111100100000"},
	{"^0.1.0-rc.6 || >=0.1.1-rc.0 <0.2.0 || >=0.1.2-a <0.2.0 || >=0.2.0-rc.1 <0.3.0-0", "11111100100000"},
	{"^0.1.0-rc.7 || ^0.1.1-rc.2 || ^0.1.2-alpha.2 || ^0.2.0-rc.1 || ^0.2.1-alpha.3 || ^0.2.1-rc.1", "11111100100000"},
	{"^1.2.3", "00000000011000"},
	{"^0.2.3", "00000000000000"},
	{"^0.0.3", "00000000000010"},
	{"~1.2", "00000000011000"},
	{"~1.2.3", "00000000011000"},
	{"~0.2", "11110000100000"},
	{"1.2.x", "00000000011000"},
	{"1.x", "00000000011000"},
	{"*", "11111111111111"},
	{">=0.2.0 <0.3.0", "11010010100000"},
	{"1.2 - 2.3", "00000000011100"},
	{"1.2.3 - 2.3.4", "00000000011100"},
	{">1", "00000000000100"},
	{"<=1.2", "11111111111011"},
	{"<=0.2.1", "11111100000011"},
	{"<0.2.1-0", "00111100000011"},
	{"=0.2.1", "01000000000000"},
	{"0.2.1", "01000000000000"},
	{"v0.2.1", "01000000000000"},
	{">=0.2.0-0 <0.2.1-0", "00110000000000"},
	{"<0.2.1", "10111100000011"},
	{">0.2", "00000011011100"},
	{">=0.2", "11110011111100"},
	{"^0", "11111111100011"},
	{"~0.0.3", "00000000000011"},
	{">= 0.2.0", "11010011111100"},
	{"^0.2.1 || *", "11111111111111"},
}

func TestSemverSatisfiesMatchesNodeSemver(t *testing.T) {
	for _, tc := range semverOracleCases {
		if len(tc.row) != len(semverOracleVersions) {
			t.Fatalf("用例 %q 的结果串长度 %d != %d", tc.rng, len(tc.row), len(semverOracleVersions))
		}
		for i, version := range semverOracleVersions {
			want := tc.row[i] == '1'
			got, supported := semverSatisfies(version, tc.rng)
			if !supported {
				t.Fatalf("范围 %q 应被支持（解析失败会让调用方放行）", tc.rng)
			}
			if got != want {
				t.Errorf("semverSatisfies(%q, %q) = %v, want %v", version, tc.rng, got, want)
			}
		}
	}
}

// 真实插件 manifest 里的范围 × 当前运行的 dsh 版本必须全部判定为兼容 ——
// 否则控制台会拒绝启用一个 dsh 自己明明在用的插件。
func TestSemverInstalledPluginRangesAreCompatible(t *testing.T) {
	const runtime = "0.2.1-rc.2"
	ranges := []string{
		">=0.2.1-rc.2",              // @linxin666/dsh-client-ui-skill-explorer peerDeps
		"^0.2.1-rc.1",               // dsh-better-sidebar peers
		">=0.1.6-alpha.2",           // dsh-ide-git engines.dsh
		"^0.1.7-rc.1 || ^0.2.0-rc.1", // dsh-free-search peers
		"^0.1.0-rc.6 || >=0.1.1-rc.0 <0.2.0 || >=0.1.2-a <0.2.0 || >=0.2.0-rc.1 <0.3.0-0", // dsh-web-mobile peers
		"^0.1.0-rc.7 || ^0.1.1-rc.2 || ^0.1.2-alpha.2 || ^0.2.0-rc.1 || ^0.2.1-alpha.3 || ^0.2.1-rc.1", // dshmarket peers
		">=0.2.0-0 <0.3.0-0", // 空格 = AND（dsh-web-mobile 的另一种写法）
	}
	for _, rng := range ranges {
		ok, supported := semverSatisfies(runtime, rng)
		if !supported || !ok {
			t.Errorf("dsh %s 应满足真实插件的范围 %q（supported=%v satisfied=%v）", runtime, rng, supported, ok)
		}
	}
}

// 解析不了的写法按无效范围处理（与 node-semver 的 validRange=null → satisfies=false 一致），
// 同时用 parsed=false 让调用方记一条 WARN。
func TestSemverInvalidRangeIsUnsatisfied(t *testing.T) {
	for _, rng := range []string{"1.x.3", "not-a-range", ">=1.2.3.4", "workspace:^", "1.2-alpha", ">=0.2-alpha <1"} {
		got, parsed := semverSatisfies("1.2.3", rng)
		if got || parsed {
			t.Errorf("范围 %q 应按无效处理（satisfied=%v parsed=%v）", rng, got, parsed)
		}
	}
}

// 预发布比较：数字标识符按数值、字母按字典序，且无预发布后缀更高。
func TestCompareSemVersionPrerelease(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-beta.2", "1.0.0-beta.11", -1},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
	}
	for _, tc := range cases {
		a, ok := parseSemVersion(tc.a)
		if !ok {
			t.Fatalf("解析 %q 失败", tc.a)
		}
		b, ok := parseSemVersion(tc.b)
		if !ok {
			t.Fatalf("解析 %q 失败", tc.b)
		}
		if got := compareSemVersion(a, b); got != tc.want {
			t.Errorf("compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// 空范围按 node-semver 的语义不满足（官方 satisfies 前还有 range.trim() !== "" 的判据）。
func TestSemverEmptyRangeIsUnsatisfied(t *testing.T) {
	if ok, supported := semverSatisfies("0.2.1", "  "); ok || !supported {
		t.Fatalf("空范围应为 (false, true)，实得 (%v, %v)", ok, supported)
	}
}

// 确保范围展开不会因为 `~>` 这类别名而 panic（解析结果与 `~` 相同）。
func TestSemverTildeAlias(t *testing.T) {
	for _, version := range []string{"0.2.1", "0.2.2-alpha.1", "0.2.9"} {
		plain, ok1 := semverSatisfies(version, "~0.2.1")
		alias, ok2 := semverSatisfies(version, "~>0.2.1")
		if !ok1 || !ok2 || !plain || !alias {
			t.Errorf("%q 应同时满足 ~0.2.1 与 ~>0.2.1（%v/%v %v/%v）", version, plain, ok1, alias, ok2)
		}
	}
}
