// fnosApp.ts —— 判断当前页面是否运行在「飞牛 App」的内置 WebView 里。
//
// 飞牛 App 的 WebView 在 UA 里带 `FNAppType/<类型>`；桌面浏览器与普通手机浏览器都没有。
// 用途：App 内某些依赖宿主桥接的能力（如目录授权）与浏览器行为不同 —— 目录授权在
// App 里改走平台的网页授权页（见 utils/fnosAuth.ts），不再调用宿主 pickUserFile。
export function isFnosApp(): boolean {
  return /FNAppType\//.test(navigator.userAgent || '')
}
