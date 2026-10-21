// semver.go —— 只为「插件兼容性判定」服务的迷你 SemVer 2.0 / npm 范围实现。
//
// 为什么自己写：官方插件管理器在启停插件时用 node-semver 的
// `satisfies(version, range, { includePrerelease: true })` 判定 `engines.dsh` /
// `peerDependencies` 里的范围（见 @deepseek-ai/dsh-app-boot 的 unmetRequirements）。
// 控制台是 Go 二进制，既不能把 dsh 的 JS 依赖带进来，也不该为一条判定引入第三方
// semver 库（构建要能离线、依赖要少）。因此这里只实现该判定用得到的子集：
//
//   - 完整版本 `x.y.z[-prerelease]`，可带 `+` 开头的构建元数据（比较时忽略）；
//   - 比较符 `=` `>` `>=` `<` `<=`，范围运算符 `^` `~` `~>`；
//   - 通配 `*` `x` `X` 与部分版本（`1` / `1.2`）；
//   - 连字符范围 `1.2.3 - 2.3.4`，空格表示 AND，`||` 表示 OR。
//
// **语义对齐 node-semver 7 的 includePrerelease:true**（官方就是这么调的）：预发布
// 版本不套「同一 major.minor.patch 才放行」那条规则；范围里缺省的部分按 `-0`
// （最低预发布）补下界。扩展表见 semver_test.go，期望值由 node-semver 7.8.5 生成。
//
// 解析不了的写法按「无效范围」处理（返回 satisfied=false、parsed=false），与 node-semver
// 对 invalid range 的结论一致（官方 satisfies 就是这么返回 false 的）；调用方会额外记一条
// WARN，让解析器缺口在日志里可见（用户仍可在 dsh 自己的插件管理里启用）。

package main

import (
	"strconv"
	"strings"
)

// semVersion 是一个解析后的完整版本号。pre 为空表示稳定版。
type semVersion struct {
	major, minor, patch int
	pre                 []string
}

// prerelease0 是 node-semver 在 includePrerelease 下给「缺省上/下界」补的 `-0`。
var prerelease0 = []string{"0"}

// parseSemVersion 解析完整版本号（必须三段齐全），允许前导 `=` / `v` 与 build 元数据。
func parseSemVersion(s string) (semVersion, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "=")
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var pre []string
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre = strings.Split(s[i+1:], ".")
		s = s[:i]
		for _, id := range pre {
			if id == "" {
				return semVersion{}, false
			}
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semVersion{}, false
	}
	var nums [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || part == "" {
			return semVersion{}, false
		}
		nums[i] = n
	}
	return semVersion{major: nums[0], minor: nums[1], patch: nums[2], pre: pre}, true
}

// isNumericIdentifier 判断一个预发布标识符是不是纯数字（node 按数值比较）。
func isNumericIdentifier(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// compareSemVersion 按 SemVer 2.0 的优先级比较（build 元数据已忽略）。
func compareSemVersion(a, b semVersion) int {
	if c := compareInt(a.major, b.major); c != 0 {
		return c
	}
	if c := compareInt(a.minor, b.minor); c != 0 {
		return c
	}
	if c := compareInt(a.patch, b.patch); c != 0 {
		return c
	}
	// 无预发布 > 有预发布
	if len(a.pre) == 0 && len(b.pre) == 0 {
		return 0
	}
	if len(a.pre) == 0 {
		return 1
	}
	if len(b.pre) == 0 {
		return -1
	}
	for i := 0; i < len(a.pre) || i < len(b.pre); i++ {
		if i >= len(a.pre) {
			return -1 // a 是 b 的前缀，更短更低
		}
		if i >= len(b.pre) {
			return 1
		}
		av, bv := a.pre[i], b.pre[i]
		aNum, bNum := isNumericIdentifier(av), isNumericIdentifier(bv)
		switch {
		case av == bv:
			continue
		case aNum && bNum:
			an, _ := strconv.Atoi(av)
			bn, _ := strconv.Atoi(bv)
			return compareInt(an, bn)
		case aNum:
			return -1 // 数字 < 字母
		case bNum:
			return 1
		case av < bv:
			return -1
		default:
			return 1
		}
	}
	return 0
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// semComparator 是一条已经展开成完整版本号的比较条件（`*` 不产生条件）。
type semComparator struct {
	op      string // "=" / ">" / ">=" / "<" / "<="
	version semVersion
}

func (c semComparator) test(v semVersion) bool {
	cmp := compareSemVersion(v, c.version)
	switch c.op {
	case "=":
		return cmp == 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	}
	return false
}

// semPartial 是范围里的一段版本号：nil 表示该段缺省或是通配（`1.x` / `1`），
// 也就是 node-semver 的 X-range。
type semPartial struct {
	major, minor, patch *int
	pre                 []string
}

// parseSemPartial 解析范围里的版本片段。返回 ok=false 表示写法不受支持
// （例如 `1.x.3` 这种通配后又出现数字的非法顺序），调用方会放行。
func parseSemPartial(s string) (semPartial, bool) {
	var p semPartial
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "=")
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		p.pre = strings.Split(s[i+1:], ".")
		s = s[:i]
		for _, id := range p.pre {
			if id == "" {
				return semPartial{}, false
			}
		}
	}
	if s == "" {
		return p, true // 全通配
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return semPartial{}, false
	}
	slots := []**int{&p.major, &p.minor, &p.patch}
	wildcardSeen := false
	for i, part := range parts {
		switch part {
		case "", "*", "x", "X":
			wildcardSeen = true
			continue
		}
		if wildcardSeen {
			return semPartial{}, false // `1.x.3` 这类非法顺序
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return semPartial{}, false
		}
		v := n
		*slots[i] = &v
	}
	// node-semver 只允许「三段齐全」的版本带预发布：`1.2-alpha` / `1-alpha` 是无效范围。
	if len(p.pre) > 0 && p.patch == nil {
		return semPartial{}, false
	}
	return p, true
}

// semverSatisfies 判断 version 是否满足 npm 范围 rng。
//
// 第二个返回值是「范围是否解析成功」：false 表示本实现不认识这个写法，结果按无效范围
// 处理（不满足），调用方据此记 WARN。空范围按 node-semver 的语义同样视为不满足。
func semverSatisfies(version, rng string) (satisfied bool, supported bool) {
	v, ok := parseSemVersion(version)
	if !ok {
		// 运行时版本一定是我们自己的目录名；解析不了说明配置异常，放行。
		return false, false
	}
	rng = strings.TrimSpace(rng)
	if rng == "" {
		return false, true // node-semver：空范围不满足
	}
	alts := strings.Split(rng, "||")
	sets := make([][]semComparator, 0, len(alts))
	for _, alt := range alts {
		set, ok := expandRangeSet(alt)
		if !ok {
			// node-semver 的 Range 会把整个字符串一起解析：任一分支出错就是无效范围，
			// 不会因为别的分支能匹配而放行。
			return false, false
		}
		sets = append(sets, set)
	}
	for _, set := range sets {
		matched := true
		for _, c := range set {
			if !c.test(v) {
				matched = false
				break
			}
		}
		if matched {
			return true, true
		}
	}
	return false, true
}

// expandRangeSet 把一个 `||` 分支展开成一组 AND 的比较条件。
func expandRangeSet(expr string) ([]semComparator, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, true // 空分支 = 任意版本
	}
	// 连字符范围：`A - B`（两侧必须有空白；版本内部的 `-` 属于预发布）。
	if from, to, ok := splitHyphenRange(expr); ok {
		var out []semComparator
		lower, ok := parseSemPartial(from)
		if !ok {
			return nil, false
		}
		upper, ok := parseSemPartial(to)
		if !ok {
			return nil, false
		}
		out = append(out, hyphenLower(lower)...)
		out = append(out, hyphenUpper(upper)...)
		return out, true
	}

	fields := strings.Fields(expr)
	tokens := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		token := fields[i]
		// 允许 `>= 0.2.0` 这种运算符与版本之间带空格的写法。
		if isBareOperator(token) && i+1 < len(fields) {
			token += fields[i+1]
			i++
		}
		tokens = append(tokens, token)
	}
	var out []semComparator
	for _, token := range tokens {
		comps, ok := expandComparatorToken(token)
		if !ok {
			return nil, false
		}
		out = append(out, comps...)
	}
	return out, true
}

func isBareOperator(token string) bool {
	switch token {
	case "=", ">", ">=", "<", "<=", "^", "~", "~>":
		return true
	}
	return false
}

func splitHyphenRange(expr string) (string, string, bool) {
	fields := strings.Fields(expr)
	if len(fields) != 3 || fields[1] != "-" {
		return "", "", false
	}
	return fields[0], fields[2], true
}

// expandComparatorToken 把单个比较条件展开成一组完整版本条件。
func expandComparatorToken(token string) ([]semComparator, bool) {
	op := ""
	for _, candidate := range []string{">=", "<=", "~>", ">", "<", "=", "^", "~"} {
		if strings.HasPrefix(token, candidate) {
			op = candidate
			token = strings.TrimSpace(strings.TrimPrefix(token, candidate))
			break
		}
	}
	p, ok := parseSemPartial(token)
	if !ok {
		return nil, false
	}
	if op == "~>" {
		op = "~"
	}

	major := func() int { return *p.major }
	minor := func() int { return *p.minor }
	patch := func() int { return *p.patch }

	switch op {
	case "^":
		switch {
		case p.major == nil:
			return nil, true // ^* = 任意
		case p.minor == nil:
			return []semComparator{
				{">=", semVersion{major: major(), pre: prerelease0}},
				{"<", semVersion{major: major() + 1, pre: prerelease0}},
			}, true
		case p.patch == nil:
			if major() == 0 {
				return []semComparator{
					{">=", semVersion{major: 0, minor: minor(), pre: prerelease0}},
					{"<", semVersion{major: 0, minor: minor() + 1, pre: prerelease0}},
				}, true
			}
			return []semComparator{
				{">=", semVersion{major: major(), minor: minor(), pre: prerelease0}},
				{"<", semVersion{major: major() + 1, pre: prerelease0}},
			}, true
		default:
			lower := semVersion{major: major(), minor: minor(), patch: patch(), pre: p.pre}
			if major() == 0 && minor() == 0 {
				return []semComparator{
					{">=", lower},
					{"<", semVersion{patch: patch() + 1, pre: prerelease0}},
				}, true
			}
			if major() == 0 {
				return []semComparator{
					{">=", lower},
					{"<", semVersion{minor: minor() + 1, pre: prerelease0}},
				}, true
			}
			return []semComparator{
				{">=", lower},
				{"<", semVersion{major: major() + 1, pre: prerelease0}},
			}, true
		}
	case "~":
		switch {
		case p.major == nil:
			return nil, true
		case p.minor == nil:
			return []semComparator{
				{">=", semVersion{major: major(), pre: prerelease0}},
				{"<", semVersion{major: major() + 1, pre: prerelease0}},
			}, true
		case p.patch == nil:
			return []semComparator{
				{">=", semVersion{major: major(), minor: minor(), pre: prerelease0}},
				{"<", semVersion{major: major(), minor: minor() + 1, pre: prerelease0}},
			}, true
		default:
			return []semComparator{
				{">=", semVersion{major: major(), minor: minor(), patch: patch(), pre: p.pre}},
				{"<", semVersion{major: major(), minor: minor() + 1, pre: prerelease0}},
			}, true
		}
	case ">":
		switch {
		case p.major == nil:
			// >* 什么都不满足（node-semver 展开成 <0.0.0-0）。
			return []semComparator{{"<", semVersion{pre: prerelease0}}}, true
		case p.minor == nil:
			return []semComparator{{">=", semVersion{major: major() + 1, pre: prerelease0}}}, true
		case p.patch == nil:
			return []semComparator{{">=", semVersion{major: major(), minor: minor() + 1, pre: prerelease0}}}, true
		default:
			return []semComparator{{">", semVersion{major: major(), minor: minor(), patch: patch(), pre: p.pre}}}, true
		}
	case ">=":
		switch {
		case p.major == nil:
			return nil, true
		case p.minor == nil:
			return []semComparator{{">=", semVersion{major: major(), pre: prerelease0}}}, true
		case p.patch == nil:
			return []semComparator{{">=", semVersion{major: major(), minor: minor(), pre: prerelease0}}}, true
		default:
			return []semComparator{{">=", semVersion{major: major(), minor: minor(), patch: patch(), pre: p.pre}}}, true
		}
	case "<":
		switch {
		case p.major == nil:
			return []semComparator{{"<", semVersion{pre: prerelease0}}}, true
		case p.minor == nil:
			return []semComparator{{"<", semVersion{major: major(), pre: prerelease0}}}, true
		case p.patch == nil:
			return []semComparator{{"<", semVersion{major: major(), minor: minor(), pre: prerelease0}}}, true
		default:
			return []semComparator{{"<", semVersion{major: major(), minor: minor(), patch: patch(), pre: p.pre}}}, true
		}
	case "<=":
		switch {
		case p.major == nil:
			return nil, true
		case p.minor == nil:
			return []semComparator{{"<", semVersion{major: major() + 1, pre: prerelease0}}}, true
		case p.patch == nil:
			return []semComparator{{"<", semVersion{major: major(), minor: minor() + 1, pre: prerelease0}}}, true
		default:
			return []semComparator{{"<=", semVersion{major: major(), minor: minor(), patch: patch(), pre: p.pre}}}, true
		}
	default: // "" / "="
		switch {
		case p.major == nil:
			return nil, true
		case p.minor == nil:
			return []semComparator{
				{">=", semVersion{major: major(), pre: prerelease0}},
				{"<", semVersion{major: major() + 1, pre: prerelease0}},
			}, true
		case p.patch == nil:
			return []semComparator{
				{">=", semVersion{major: major(), minor: minor(), pre: prerelease0}},
				{"<", semVersion{major: major(), minor: minor() + 1, pre: prerelease0}},
			}, true
		default:
			return []semComparator{{"=", semVersion{major: major(), minor: minor(), patch: patch(), pre: p.pre}}}, true
		}
	}
}

// hyphenLower 是连字符范围的下界（includePrerelease：稳定版补 -0）。
func hyphenLower(p semPartial) []semComparator {
	switch {
	case p.major == nil:
		return nil
	case p.minor == nil:
		return []semComparator{{">=", semVersion{major: *p.major, pre: prerelease0}}}
	case p.patch == nil:
		return []semComparator{{">=", semVersion{major: *p.major, minor: *p.minor, pre: prerelease0}}}
	}
	v := semVersion{major: *p.major, minor: *p.minor, patch: *p.patch, pre: p.pre}
	if len(v.pre) == 0 {
		v.pre = prerelease0
	}
	return []semComparator{{">=", v}}
}

// hyphenUpper 是连字符范围的上界。
func hyphenUpper(p semPartial) []semComparator {
	switch {
	case p.major == nil:
		return nil
	case p.minor == nil:
		return []semComparator{{"<", semVersion{major: *p.major + 1, pre: prerelease0}}}
	case p.patch == nil:
		return []semComparator{{"<", semVersion{major: *p.major, minor: *p.minor + 1, pre: prerelease0}}}
	}
	if len(p.pre) > 0 {
		return []semComparator{{"<=", semVersion{major: *p.major, minor: *p.minor, patch: *p.patch, pre: p.pre}}}
	}
	return []semComparator{{"<", semVersion{major: *p.major, minor: *p.minor, patch: *p.patch + 1, pre: prerelease0}}}
}
