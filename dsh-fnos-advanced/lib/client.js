(function () {
  if (!window.__ModuleLoader__ || typeof window.__ModuleLoader__.load !== "function") {
    console.warn("[dsh-fnos-advanced] client module loader is not available");
    return;
  }
  window.__ModuleLoader__.load({
  id: "dsh-fnos-advanced",
  factory: (require) => {
    var module = { exports: {} };
    var exports = module.exports;
var __create = Object.create;
var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __getProtoOf = Object.getPrototypeOf;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __export = (target, all) => {
  for (var name in all)
    __defProp(target, name, { get: all[name], enumerable: true });
};
var __copyProps = (to, from, except, desc) => {
  if (from && typeof from === "object" || typeof from === "function") {
    for (let key of __getOwnPropNames(from))
      if (!__hasOwnProp.call(to, key) && key !== except)
        __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
  }
  return to;
};
var __toESM = (mod, isNodeMode, target) => (target = mod != null ? __create(__getProtoOf(mod)) : {}, __copyProps(
  // If the importer is in node compatibility mode or this is not an ESM
  // file that has been converted to a CommonJS file using a Babel-
  // compatible transform (i.e. "__esModule" has not been set), then set
  // "default" to the CommonJS "module.exports" for node compatibility.
  isNodeMode || !mod || !mod.__esModule ? __defProp(target, "default", { value: mod, enumerable: true }) : target,
  mod
));
var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);

// src/client/index.js
var index_exports = {};
__export(index_exports, {
  apply: () => apply
});
module.exports = __toCommonJS(index_exports);
var React = __toESM(require("react"), 1);

// node_modules/@trimjs/web-app/dist/index.js
function e(t2) {
  return e = "function" == typeof Symbol && "symbol" == typeof Symbol.iterator ? function(e2) {
    return typeof e2;
  } : function(e2) {
    return e2 && "function" == typeof Symbol && e2.constructor === Symbol && e2 !== Symbol.prototype ? "symbol" : typeof e2;
  }, e(t2);
}
function t(t2) {
  var r2 = (function(t3, r3) {
    if ("object" != e(t3) || !t3) return t3;
    var n2 = t3[Symbol.toPrimitive];
    if (void 0 !== n2) {
      var i2 = n2.call(t3, r3 || "default");
      if ("object" != e(i2)) return i2;
      throw new TypeError("@@toPrimitive must return a primitive value.");
    }
    return ("string" === r3 ? String : Number)(t3);
  })(t2, "string");
  return "symbol" == e(r2) ? r2 : r2 + "";
}
function r(e2, r2, n2) {
  return (r2 = t(r2)) in e2 ? Object.defineProperty(e2, r2, { value: n2, enumerable: true, configurable: true, writable: true }) : e2[r2] = n2, e2;
}
var n = new class {
  constructor() {
    r(this, "debug", false);
  }
  setDebug(e2) {
    this.debug = e2;
  }
  isDebug() {
    return this.debug;
  }
  log() {
    if (this.debug) {
      for (var e2 = arguments.length, t2 = new Array(e2), r2 = 0; r2 < e2; r2++) t2[r2] = arguments[r2];
      console.log("[Trim App]", ...t2);
    }
  }
  warn() {
    if (this.debug) {
      for (var e2 = arguments.length, t2 = new Array(e2), r2 = 0; r2 < e2; r2++) t2[r2] = arguments[r2];
      console.warn("[Trim App]", ...t2);
    }
  }
  error() {
    if (this.debug) {
      for (var e2 = arguments.length, t2 = new Array(e2), r2 = 0; r2 < e2; r2++) t2[r2] = arguments[r2];
      console.error("[Trim App]", ...t2);
    }
  }
  info() {
    if (this.debug) {
      for (var e2 = arguments.length, t2 = new Array(e2), r2 = 0; r2 < e2; r2++) t2[r2] = arguments[r2];
      console.info("[Trim App]", ...t2);
    }
  }
}();
var i = () => (function(e2) {
  const t2 = /FNOS\/([\d.]+)|FNAppType\/(\w+)|FNAppVer\/([\d.]+)/g, r2 = {};
  let n2;
  for (; null !== (n2 = t2.exec(e2)); ) n2[1] && (r2.fnOSVersion = n2[1]), n2[2] && (r2.fnAppType = n2[2]), n2[3] && (r2.fnAppVersion = n2[3]);
  return r2;
})(navigator.userAgent).fnAppVersion;
function o(e2, t2, r2, n2, i2, o2, s2) {
  try {
    var l2 = e2[o2](s2), a2 = l2.value;
  } catch (e3) {
    return void r2(e3);
  }
  l2.done ? t2(a2) : Promise.resolve(a2).then(n2, i2);
}
function s(e2) {
  return function() {
    var t2 = this, r2 = arguments;
    return new Promise(function(n2, i2) {
      var s2 = e2.apply(t2, r2);
      function l2(e3) {
        o(s2, n2, i2, l2, a2, "next", e3);
      }
      function a2(e3) {
        o(s2, n2, i2, l2, a2, "throw", e3);
      }
      l2(void 0);
    });
  };
}
function l(e2, t2) {
  var r2 = Object.keys(e2);
  if (Object.getOwnPropertySymbols) {
    var n2 = Object.getOwnPropertySymbols(e2);
    t2 && (n2 = n2.filter(function(t3) {
      return Object.getOwnPropertyDescriptor(e2, t3).enumerable;
    })), r2.push.apply(r2, n2);
  }
  return r2;
}
function a(e2) {
  for (var t2 = 1; t2 < arguments.length; t2++) {
    var n2 = null != arguments[t2] ? arguments[t2] : {};
    t2 % 2 ? l(Object(n2), true).forEach(function(t3) {
      r(e2, t3, n2[t3]);
    }) : Object.getOwnPropertyDescriptors ? Object.defineProperties(e2, Object.getOwnPropertyDescriptors(n2)) : l(Object(n2)).forEach(function(t3) {
      Object.defineProperty(e2, t3, Object.getOwnPropertyDescriptor(n2, t3));
    });
  }
  return e2;
}
var u = false;
window.addEventListener("flutterInAppWebViewPlatformReady", () => {
  u = true;
});
var c = /(iPad|iPhone|iPod|Macintosh|Mac OS X)/i.test(navigator.userAgent);
var p = /* @__PURE__ */ (function() {
  var e2 = s(function* (e3, t2) {
    for (var r2 = arguments.length, i2 = new Array(r2 > 2 ? r2 - 2 : 0), o2 = 2; o2 < r2; o2++) i2[o2 - 2] = arguments[o2];
    n.log(`callHandler: ${t2}, params: ${i2.map((e4) => String(e4)).join(", ")}`);
    const s2 = yield e3.callHandler(t2, ...i2);
    if (s2) try {
      const e4 = JSON.parse(s2);
      n.log(`messageData: ${JSON.stringify(e4)}`);
      const t3 = JSON.parse(e4.result);
      return n.log(`appMessage: ${JSON.stringify(t3)}`), t3;
    } catch (l2) {
      return n.error(`error: ${l2}`), null;
    }
    return null;
  });
  return function(t2, r2) {
    return e2.apply(this, arguments);
  };
})();
var d = /* @__PURE__ */ new Map();
var f = class {
  constructor(e2) {
    r(this, "subscribeFn", void 0), this.subscribeFn = e2;
  }
  subscribe(e2, t2, r2) {
    var n2;
    const i2 = "function" == typeof e2 ? { next: e2, error: t2, complete: r2 } : null != e2 ? e2 : {};
    let o2 = false;
    const s2 = null !== (n2 = this.subscribeFn({ next: (e3) => {
      var t3;
      o2 || (null === (t3 = i2.next) || void 0 === t3 || t3.call(i2, e3));
    }, error: (e3) => {
      var t3;
      o2 || (o2 = true, null === (t3 = i2.error) || void 0 === t3 || t3.call(i2, e3));
    }, complete: () => {
      var e3;
      o2 || (o2 = true, null === (e3 = i2.complete) || void 0 === e3 || e3.call(i2));
    } })) && void 0 !== n2 ? n2 : () => {
    };
    return { unsubscribe: () => {
      o2 || (o2 = true, s2());
    } };
  }
};
var h = /* @__PURE__ */ new Map();
function v() {
  window._flutter_onAppObservableQueryResult = (e2) => {
    n.log("_flutter_onAppObservableQueryResult", e2);
    const t2 = (function(e3) {
      var t3, r3;
      const n2 = "string" == typeof e3 ? JSON.parse(e3) : e3, i2 = null !== (t3 = n2.reqid) && void 0 !== t3 ? t3 : n2.reqId;
      return a(a({}, n2), {}, { reqId: null !== (r3 = n2.reqId) && void 0 !== r3 ? r3 : i2, reqid: i2 });
    })(e2), r2 = h.get(t2.reqId);
    if (r2) if (t2.result && "doing" !== t2.result) {
      if ("succ" === t2.result) return r2.next(t2), r2.complete(), void h.delete(t2.reqId);
      r2.error(t2), h.delete(t2.reqId);
    } else r2.next(t2);
    else n.log("_flutter_onAppObservableQueryResult observer not found", t2);
  };
}
function y() {
  return (y = s(function* (e2, t2) {
    return new Promise((r2, i2) => {
      null == e2 || e2.callHandler("openFolder", JSON.stringify(t2)).then((e3) => {
        if (n.log("pickFilePromise callback registered", { reqId: e3, params: t2 }), e3) {
          let o2 = function(e4) {
            r2(e4.result);
          };
          d.set(e3, o2);
        } else i2(/* @__PURE__ */ new Error("openFolder failed"));
      });
    });
  })).apply(this, arguments);
}
i() && (window.fnAppMessage = (e2) => {
  n.log("app-message", e2);
  const t2 = JSON.parse(e2), r2 = d.get(t2.reqId);
  r2 && (r2(t2), d.delete(t2.reqId));
}), i() && v();
var g = /* @__PURE__ */ (function(e2) {
  return e2.Call = "call", e2.Reply = "reply", e2.Syn = "syn", e2.SynAck = "synAck", e2.Ack = "ack", e2;
})({});
var b = /* @__PURE__ */ (function(e2) {
  return e2.Fulfilled = "fulfilled", e2.Rejected = "rejected", e2;
})({});
var m = /* @__PURE__ */ (function(e2) {
  return e2.ConnectionDestroyed = "ConnectionDestroyed", e2.ConnectionTimeout = "ConnectionTimeout", e2.NoIframeSrc = "NoIframeSrc", e2;
})({});
var w = /* @__PURE__ */ (function(e2) {
  return e2.DataCloneError = "DataCloneError", e2;
})({});
var A = /* @__PURE__ */ (function(e2) {
  return e2.Message = "message", e2;
})({});
var S = ({ name: e2, message: t2, stack: r2 }) => ({ name: e2, message: t2, stack: r2 });
var O = (e2, t2, r2) => {
  let { localName: n2, local: i2, remote: o2, originForSending: s2, originForReceiving: l2 } = e2, a2 = false, u2 = (e3) => {
    if (e3.source !== o2 || e3.data.penpal !== g.Call) return;
    if ("*" !== l2 && e3.origin !== l2) return void r2(`${n2} received message from origin ${e3.origin} which did not match expected origin ${l2}`);
    let { methodName: i3, args: u3, id: c2 } = e3.data;
    r2(`${n2}: Received ${i3}() call`);
    let p2 = (e4) => (t3) => {
      if (r2(`${n2}: Sending ${i3}() reply`), a2) return void r2(`${n2}: Unable to send ${i3}() reply due to destroyed connection`);
      let l3 = { penpal: g.Reply, id: c2, resolution: e4, returnValue: t3 };
      e4 === b.Rejected && t3 instanceof Error && (l3.returnValue = S(t3), l3.returnValueIsError = true);
      try {
        o2.postMessage(l3, s2);
      } catch (e5) {
        let r3 = e5 instanceof Error ? e5 : Error(String(e5));
        if (r3.name === w.DataCloneError) {
          let e6 = { penpal: g.Reply, id: c2, resolution: b.Rejected, returnValue: S(r3), returnValueIsError: true };
          o2.postMessage(e6, s2);
        }
        throw e5;
      }
    };
    new Promise((e4) => e4(t2[i3].apply(t2, u3))).then(p2(b.Fulfilled), p2(b.Rejected));
  };
  return i2.addEventListener(A.Message, u2), () => {
    a2 = true, i2.removeEventListener(A.Message, u2);
  };
};
var P = 0;
var E = () => ++P;
var M = (e2) => e2 ? e2.split(".") : [];
var F = (e2, t2, r2) => {
  let n2 = M(t2);
  return n2.reduce((e3, t3, i2) => (void 0 === e3[t3] && (e3[t3] = {}), i2 === n2.length - 1 && (e3[t3] = r2), e3[t3]), e2), e2;
};
var $ = (e2, t2) => {
  let r2 = {};
  return Object.keys(e2).forEach((n2) => {
    let i2 = e2[n2], o2 = ((e3, t3) => {
      let r3 = M(t3 || "");
      return r3.push(e3), ((e4) => e4.join("."))(r3);
    })(n2, t2);
    "object" == typeof i2 && Object.assign(r2, $(i2, o2)), "function" == typeof i2 && (r2[o2] = i2);
  }), r2;
};
var _ = (e2, t2, r2, n2, i2) => {
  let { localName: o2, local: s2, remote: l2, originForSending: a2, originForReceiving: u2 } = t2, c2 = false, p2 = /* @__PURE__ */ new Set();
  i2(`${o2}: Connecting call sender`);
  let d2 = (e3) => (...r3) => {
    let d3;
    i2(`${o2}: Sending ${e3}() call`);
    try {
      l2.closed && (d3 = true);
    } catch (g2) {
      d3 = true;
    }
    if (d3 && n2(), c2) {
      let t3 = /* @__PURE__ */ Error(`Unable to send ${e3}() call due to destroyed connection`);
      throw t3.code = m.ConnectionDestroyed, t3;
    }
    return new Promise((n3, c3) => {
      var d4;
      let f3, h2 = E(), v2 = () => {
        s2.removeEventListener(A.Message, m2), void 0 !== f3 && clearTimeout(f3), p2.delete(y2);
      }, y2 = () => {
        v2(), c3(/* @__PURE__ */ Error("Connection closed"));
      }, m2 = (t3) => {
        var r4;
        if (t3.source !== l2 || (null == (r4 = t3.data) ? void 0 : r4.penpal) !== g.Reply || t3.data.id !== h2) return;
        if ("*" !== u2 && t3.origin !== u2) return void i2(`${o2} received message from origin ${t3.origin} which did not match expected origin ${u2}`);
        let s3 = t3.data;
        i2(`${o2}: Received ${e3}() reply`), v2();
        let a3 = s3.returnValue;
        s3.returnValueIsError && (a3 = ((e4) => {
          let t4 = /* @__PURE__ */ Error();
          return Object.assign(t4, e4), t4;
        })(a3)), (s3.resolution === b.Fulfilled ? n3 : c3)(a3);
      };
      s2.addEventListener(A.Message, m2), p2.add(y2);
      let w2 = null == (d4 = t2.callTimeouts) ? void 0 : d4[e3];
      void 0 !== w2 && (f3 = setTimeout(y2, w2));
      let S2 = { penpal: g.Call, id: h2, methodName: e3, args: r3 };
      try {
        l2.postMessage(S2, a2);
      } catch (g2) {
        v2(), c3(g2);
      }
    });
  }, f2 = r2.reduce((e3, t3) => (e3[t3] = d2(t3), e3), {});
  return Object.assign(e2, ((e3) => {
    let t3 = {};
    for (let r3 in e3) F(t3, r3, e3[r3]);
    return t3;
  })(f2)), () => {
    c2 = true;
    for (let e3 of p2) e3();
  };
};
var k = (e2, t2) => {
  let r2;
  return void 0 !== e2 && (r2 = window.setTimeout(() => {
    let r3 = /* @__PURE__ */ Error(`Connection timed out after ${e2}ms`);
    r3.code = m.ConnectionTimeout, t2(r3);
  }, e2)), () => {
    clearTimeout(r2);
  };
};
function T(e2) {
  return T = "function" == typeof Symbol && "symbol" == typeof Symbol.iterator ? function(e3) {
    return typeof e3;
  } : function(e3) {
    return e3 && "function" == typeof Symbol && e3.constructor === Symbol && e3 !== Symbol.prototype ? "symbol" : typeof e3;
  }, T(e2);
}
function j(e2) {
  var t2 = (function(e3, t3) {
    if ("object" != T(e3) || !e3) return e3;
    var r2 = e3[Symbol.toPrimitive];
    if (void 0 !== r2) {
      var n2 = r2.call(e3, t3 || "default");
      if ("object" != T(n2)) return n2;
      throw TypeError("@@toPrimitive must return a primitive value.");
    }
    return ("string" === t3 ? String : Number)(e3);
  })(e2, "string");
  return "symbol" == T(t2) ? t2 : t2 + "";
}
function N(e2, t2, r2) {
  return (t2 = j(t2)) in e2 ? Object.defineProperty(e2, t2, { value: r2, enumerable: true, configurable: true, writable: true }) : e2[t2] = r2, e2;
}
function W(e2, t2) {
  var r2 = Object.keys(e2);
  if (Object.getOwnPropertySymbols) {
    var n2 = Object.getOwnPropertySymbols(e2);
    t2 && (n2 = n2.filter(function(t3) {
      return Object.getOwnPropertyDescriptor(e2, t3).enumerable;
    })), r2.push.apply(r2, n2);
  }
  return r2;
}
function C(e2) {
  for (var t2 = 1; t2 < arguments.length; t2++) {
    var r2 = null == arguments[t2] ? {} : arguments[t2];
    t2 % 2 ? W(Object(r2), true).forEach(function(t3) {
      N(e2, t3, r2[t3]);
    }) : Object.getOwnPropertyDescriptors ? Object.defineProperties(e2, Object.getOwnPropertyDescriptors(r2)) : W(Object(r2)).forEach(function(t3) {
      Object.defineProperty(e2, t3, Object.getOwnPropertyDescriptor(r2, t3));
    });
  }
  return e2;
}
var q = (e2, t2, r2, n2, i2, o2) => {
  let { destroy: s2, onDestroy: l2 } = n2;
  return (n3) => {
    if (!(e2 instanceof RegExp ? e2.test(n3.origin) : "*" === e2 || e2 === n3.origin)) return void i2(`Child: Handshake - Received SYN-ACK from origin ${n3.origin} which did not match expected origin ${e2}`);
    i2("Child: Handshake - Received SYN-ACK, responding with ACK");
    let a2 = "null" === n3.origin ? "*" : n3.origin, u2 = { penpal: g.Ack, methodNames: Object.keys(t2), config: r2 };
    window.parent.postMessage(u2, a2);
    let c2 = { callTimeouts: o2, localName: "Child", local: window, remote: window.parent, originForSending: a2, originForReceiving: n3.origin }, p2 = O(c2, t2, i2);
    l2(p2);
    let d2 = {}, f2 = _(d2, c2, n3.data.methodNames, s2, i2);
    return l2(f2), d2;
  };
};
function x(e2, t2) {
  var r2;
  null == (r2 = console) || r2.warn(`[fnApp warn]: ${e2}`, t2);
}
function I(e2, t2) {
  var r2;
  null == (r2 = console) || r2.error(`[fnApp error]: ${e2}`, t2);
}
var U = "_fn_all_event";
var V = "事件订阅数量为空";
function R(e2) {
  return R = "function" == typeof Symbol && "symbol" == typeof Symbol.iterator ? function(e3) {
    return typeof e3;
  } : function(e3) {
    return e3 && "function" == typeof Symbol && e3.constructor === Symbol && e3 !== Symbol.prototype ? "symbol" : typeof e3;
  }, R(e2);
}
function D(e2) {
  var t2 = (function(e3, t3) {
    if ("object" != R(e3) || !e3) return e3;
    var r2 = e3[Symbol.toPrimitive];
    if (void 0 !== r2) {
      var n2 = r2.call(e3, t3 || "default");
      if ("object" != R(n2)) return n2;
      throw TypeError("@@toPrimitive must return a primitive value.");
    }
    return ("string" === t3 ? String : Number)(e3);
  })(e2, "string");
  return "symbol" == R(t2) ? t2 : t2 + "";
}
function z(e2, t2, r2) {
  return (t2 = D(t2)) in e2 ? Object.defineProperty(e2, t2, { value: r2, enumerable: true, configurable: true, writable: true }) : e2[t2] = r2, e2;
}
var L = window.__POWERED_BY_FNAPP__ ? window.__FNAPP.inject.appEventObjMap : /* @__PURE__ */ new Map();
var J = class {
  constructor(e2, t2) {
    z(this, "id", void 0), z(this, "appName", void 0), z(this, "eventObj", void 0), z(this, "getEventCallBackLength", (e3) => {
      var t3;
      return (null == (t3 = this.eventObj[e3]) ? void 0 : t3.length) || 0;
    }), this.id = e2, this.appName = t2, this.$clear(), L.get(this.id) || L.set(this.id, {}), this.eventObj = L.get(this.id);
  }
  $on(e2, t2) {
    let r2 = this.eventObj[e2];
    return r2 ? (r2.includes(t2) || r2.push(t2), this) : (this.eventObj[e2] = [t2], this);
  }
  $onAll(e2) {
    return this.$on(U, e2);
  }
  $once(e2, t2) {
    t2.$__once = true, this.$on(e2, t2);
  }
  $off(e2, t2) {
    let r2 = this.eventObj[e2];
    if (!e2 || !r2 || !r2.length) return x(`${e2} ${V}`), this;
    let n2, i2 = r2.length;
    for (; i2--; ) if (n2 = r2[i2], n2 === t2) {
      r2.splice(i2, 1);
      break;
    }
    return this;
  }
  $offAll(e2) {
    return this.$off(U, e2);
  }
  $emit(e2, ...t2) {
    return e2 = `${this.appName}/${e2}`, this.$pureEmit(e2, ...t2);
  }
  $pureEmit(e2, ...t2) {
    let r2 = 0;
    return L.forEach((n2) => {
      if (n2[e2]) for (let i2 = n2[e2].length - 1; i2 >= 0; i2--) {
        r2++;
        let o2 = n2[e2][i2];
        o2.$__once && n2[e2].splice(i2, 1);
        try {
          o2(...t2);
        } catch (e3) {
          I(e3);
        }
      }
      if (n2._fn_all_event) for (let i2 = n2[U].length - 1; i2 >= 0; i2--) {
        r2++;
        let o2 = n2[U][i2];
        o2.$__once && n2[U].splice(i2, 1);
        try {
          o2(...t2);
        } catch (e3) {
          I(e3);
        }
      }
    }), (!e2 || 0 === r2) && x(`${e2} ${V}`), this;
  }
  $clear() {
    var e2;
    let t2 = null == (e2 = L.get(this.id)) ? {} : e2;
    return Object.keys(t2).forEach((e3) => delete t2[e3]), this;
  }
};
var H = function(e2, t2) {
  return H = Object.setPrototypeOf || { __proto__: [] } instanceof Array && function(e3, t3) {
    e3.__proto__ = t3;
  } || function(e3, t3) {
    for (var r2 in t3) Object.prototype.hasOwnProperty.call(t3, r2) && (e3[r2] = t3[r2]);
  }, H(e2, t2);
};
function Q(e2, t2) {
  if ("function" != typeof t2 && null !== t2) throw TypeError("Class extends value " + String(t2) + " is not a constructor or null");
  function r2() {
    this.constructor = e2;
  }
  H(e2, t2), e2.prototype = null === t2 ? Object.create(t2) : (r2.prototype = t2.prototype, new r2());
}
function B(e2) {
  var t2 = "function" == typeof Symbol && Symbol.iterator, r2 = t2 && e2[t2], n2 = 0;
  if (r2) return r2.call(e2);
  if (e2 && "number" == typeof e2.length) return { next: function() {
    return e2 && n2 >= e2.length && (e2 = void 0), { value: e2 && e2[n2++], done: !e2 };
  } };
  throw TypeError(t2 ? "Object is not iterable." : "Symbol.iterator is not defined.");
}
function Y(e2, t2) {
  var r2 = "function" == typeof Symbol && e2[Symbol.iterator];
  if (!r2) return e2;
  var n2, i2, o2 = r2.call(e2), s2 = [];
  try {
    for (; (void 0 === t2 || t2-- > 0) && !(n2 = o2.next()).done; ) s2.push(n2.value);
  } catch (e3) {
    i2 = { error: e3 };
  } finally {
    try {
      n2 && !n2.done && (r2 = o2.return) && r2.call(o2);
    } finally {
      if (i2) throw i2.error;
    }
  }
  return s2;
}
function G(e2, t2, r2) {
  if (r2 || 2 === arguments.length) for (var n2, i2 = 0, o2 = t2.length; i2 < o2; i2++) (n2 || !(i2 in t2)) && (n2 || (n2 = Array.prototype.slice.call(t2, 0, i2)), n2[i2] = t2[i2]);
  return e2.concat(n2 || Array.prototype.slice.call(t2));
}
function K(e2) {
  return "function" == typeof e2;
}
var X = (function(e2) {
  var t2 = e2(function(e3) {
    Error.call(e3), e3.stack = Error().stack;
  });
  return t2.prototype = Object.create(Error.prototype), t2.prototype.constructor = t2, t2;
})(function(e2) {
  return function(t2) {
    e2(this), this.message = t2 ? t2.length + " errors occurred during unsubscription:\n" + t2.map(function(e3, t3) {
      return t3 + 1 + ") " + e3.toString();
    }).join("\n  ") : "", this.name = "UnsubscriptionError", this.errors = t2;
  };
});
function Z(e2, t2) {
  if (e2) {
    var r2 = e2.indexOf(t2);
    0 <= r2 && e2.splice(r2, 1);
  }
}
var ee = (function() {
  function e2(e3) {
    this.initialTeardown = e3, this.closed = false, this._parentage = null, this._finalizers = null;
  }
  return e2.prototype.unsubscribe = function() {
    var e3, t2, r2, n2, i2;
    if (!this.closed) {
      this.closed = true;
      var o2 = this._parentage;
      if (o2) if (this._parentage = null, Array.isArray(o2)) try {
        for (var s2 = B(o2), l2 = s2.next(); !l2.done; l2 = s2.next()) l2.value.remove(this);
      } catch (t3) {
        e3 = { error: t3 };
      } finally {
        try {
          l2 && !l2.done && (t2 = s2.return) && t2.call(s2);
        } finally {
          if (e3) throw e3.error;
        }
      }
      else o2.remove(this);
      var a2 = this.initialTeardown;
      if (K(a2)) try {
        a2();
      } catch (e4) {
        i2 = e4 instanceof X ? e4.errors : [e4];
      }
      var u2 = this._finalizers;
      if (u2) {
        this._finalizers = null;
        try {
          for (var c2 = B(u2), p2 = c2.next(); !p2.done; p2 = c2.next()) {
            var d2 = p2.value;
            try {
              re(d2);
            } catch (e4) {
              i2 = null == i2 ? [] : i2, e4 instanceof X ? i2 = G(G([], Y(i2)), Y(e4.errors)) : i2.push(e4);
            }
          }
        } catch (e4) {
          r2 = { error: e4 };
        } finally {
          try {
            p2 && !p2.done && (n2 = c2.return) && n2.call(c2);
          } finally {
            if (r2) throw r2.error;
          }
        }
      }
      if (i2) throw new X(i2);
    }
  }, e2.prototype.add = function(t2) {
    var r2;
    if (t2 && t2 !== this) if (this.closed) re(t2);
    else {
      if (t2 instanceof e2) {
        if (t2.closed || t2._hasParent(this)) return;
        t2._addParent(this);
      }
      (this._finalizers = null == (r2 = this._finalizers) ? [] : r2).push(t2);
    }
  }, e2.prototype._hasParent = function(e3) {
    var t2 = this._parentage;
    return t2 === e3 || Array.isArray(t2) && t2.includes(e3);
  }, e2.prototype._addParent = function(e3) {
    var t2 = this._parentage;
    this._parentage = Array.isArray(t2) ? (t2.push(e3), t2) : t2 ? [t2, e3] : e3;
  }, e2.prototype._removeParent = function(e3) {
    var t2 = this._parentage;
    t2 === e3 ? this._parentage = null : Array.isArray(t2) && Z(t2, e3);
  }, e2.prototype.remove = function(t2) {
    var r2 = this._finalizers;
    r2 && Z(r2, t2), t2 instanceof e2 && t2._removeParent(this);
  }, e2.EMPTY = (function() {
    var t2 = new e2();
    return t2.closed = true, t2;
  })(), e2;
})();
function te(e2) {
  return e2 instanceof ee || e2 && "closed" in e2 && K(e2.remove) && K(e2.add) && K(e2.unsubscribe);
}
function re(e2) {
  K(e2) ? e2() : e2.unsubscribe();
}
ee.EMPTY;
var ne = null;
var ie = null;
var oe = void 0;
var se = false;
var le = false;
var ae = { setTimeout: function(e2, t2) {
  var r2 = [...arguments].slice(2), n2 = ae.delegate;
  return null != n2 && n2.setTimeout ? n2.setTimeout.apply(n2, G([e2, t2], Y(r2))) : setTimeout.apply(void 0, G([e2, t2], Y(r2)));
}, clearTimeout: function(e2) {
  var t2 = ae.delegate;
  return ((null == t2 ? void 0 : t2.clearTimeout) || clearTimeout)(e2);
}, delegate: void 0 };
function ue() {
}
var ce = pe("C", void 0, void 0);
function pe(e2, t2, r2) {
  return { kind: e2, value: t2, error: r2 };
}
var de = null;
var fe = (function(e2) {
  function t2(t3) {
    var r2 = e2.call(this) || this;
    return r2.isStopped = false, t3 ? (r2.destination = t3, te(t3) && t3.add(r2)) : r2.destination = we, r2;
  }
  return Q(t2, e2), t2.create = function(e3, t3, r2) {
    return new ge(e3, t3, r2);
  }, t2.prototype.next = function(e3) {
    this.isStopped ? me((function(e4) {
      return pe("N", e4, void 0);
    })(e3), this) : this._next(e3);
  }, t2.prototype.error = function(e3) {
    this.isStopped ? me((function(e4) {
      return pe("E", void 0, e4);
    })(e3), this) : (this.isStopped = true, this._error(e3));
  }, t2.prototype.complete = function() {
    this.isStopped ? me(ce, this) : (this.isStopped = true, this._complete());
  }, t2.prototype.unsubscribe = function() {
    this.closed || (this.isStopped = true, e2.prototype.unsubscribe.call(this), this.destination = null);
  }, t2.prototype._next = function(e3) {
    this.destination.next(e3);
  }, t2.prototype._error = function(e3) {
    try {
      this.destination.error(e3);
    } finally {
      this.unsubscribe();
    }
  }, t2.prototype._complete = function() {
    try {
      this.destination.complete();
    } finally {
      this.unsubscribe();
    }
  }, t2;
})(ee);
var he = Function.prototype.bind;
function ve(e2, t2) {
  return he.call(e2, t2);
}
var ye = (function() {
  function e2(e3) {
    this.partialObserver = e3;
  }
  return e2.prototype.next = function(e3) {
    var t2 = this.partialObserver;
    if (t2.next) try {
      t2.next(e3);
    } catch (e4) {
      be(e4);
    }
  }, e2.prototype.error = function(e3) {
    var t2 = this.partialObserver;
    if (t2.error) try {
      t2.error(e3);
    } catch (e4) {
      be(e4);
    }
    else be(e3);
  }, e2.prototype.complete = function() {
    var e3 = this.partialObserver;
    if (e3.complete) try {
      e3.complete();
    } catch (e4) {
      be(e4);
    }
  }, e2;
})();
var ge = (function(e2) {
  function t2(t3, r2, n2) {
    var i2, o2, s2 = e2.call(this) || this;
    K(t3) || !t3 ? i2 = { next: null == t3 ? void 0 : t3, error: null == r2 ? void 0 : r2, complete: null == n2 ? void 0 : n2 } : s2 && le ? ((o2 = Object.create(t3)).unsubscribe = function() {
      return s2.unsubscribe();
    }, i2 = { next: t3.next && ve(t3.next, o2), error: t3.error && ve(t3.error, o2), complete: t3.complete && ve(t3.complete, o2) }) : i2 = t3;
    return s2.destination = new ye(i2), s2;
  }
  return Q(t2, e2), t2;
})(fe);
function be(e2) {
  se ? (function(e3) {
    se && de && (de.errorThrown = true, de.error = e3);
  })(e2) : (function(e3) {
    ae.setTimeout(function() {
      if (!ne) throw e3;
      ne(e3);
    });
  })(e2);
}
function me(e2, t2) {
  var r2 = ie;
  r2 && ae.setTimeout(function() {
    return r2(e2, t2);
  });
}
var we = { closed: true, next: ue, error: function(e2) {
  throw e2;
}, complete: ue };
var Ae = "function" == typeof Symbol && Symbol.observable || "@@observable";
function Se(e2) {
  return e2;
}
var Oe = (function() {
  function e2(e3) {
    e3 && (this._subscribe = e3);
  }
  return e2.prototype.lift = function(t2) {
    var r2 = new e2();
    return r2.source = this, r2.operator = t2, r2;
  }, e2.prototype.subscribe = function(e3, t2, r2) {
    var n2 = this, i2 = (function(e4) {
      return e4 && e4 instanceof fe || (function(e5) {
        return e5 && K(e5.next) && K(e5.error) && K(e5.complete);
      })(e4) && te(e4);
    })(e3) ? e3 : new ge(e3, t2, r2);
    return (function(e4) {
      if (se) {
        var t3 = !de;
        if (t3 && (de = { errorThrown: false, error: null }), e4(), t3) {
          var r3 = de, n3 = r3.errorThrown, i3 = r3.error;
          if (de = null, n3) throw i3;
        }
      } else e4();
    })(function() {
      var e4 = n2, t3 = e4.operator, r3 = e4.source;
      i2.add(t3 ? t3.call(i2, r3) : r3 ? n2._subscribe(i2) : n2._trySubscribe(i2));
    }), i2;
  }, e2.prototype._trySubscribe = function(e3) {
    try {
      return this._subscribe(e3);
    } catch (b2) {
      e3.error(b2);
    }
  }, e2.prototype.forEach = function(e3, t2) {
    var r2 = this;
    return new (t2 = Pe(t2))(function(t3, n2) {
      var i2 = new ge({ next: function(t4) {
        try {
          e3(t4);
        } catch (e4) {
          n2(e4), i2.unsubscribe();
        }
      }, error: n2, complete: t3 });
      r2.subscribe(i2);
    });
  }, e2.prototype._subscribe = function(e3) {
    var t2;
    return null == (t2 = this.source) ? void 0 : t2.subscribe(e3);
  }, e2.prototype[Ae] = function() {
    return this;
  }, e2.prototype.pipe = function() {
    return (function(e3) {
      return 0 === e3.length ? Se : 1 === e3.length ? e3[0] : function(t2) {
        return e3.reduce(function(e4, t3) {
          return t3(e4);
        }, t2);
      };
    })([...arguments])(this);
  }, e2.prototype.toPromise = function(e3) {
    var t2 = this;
    return new (e3 = Pe(e3))(function(e4, r2) {
      var n2;
      t2.subscribe(function(e5) {
        return n2 = e5;
      }, function(e5) {
        return r2(e5);
      }, function() {
        return e4(n2);
      });
    });
  }, e2.create = function(t2) {
    return new e2(t2);
  }, e2;
})();
function Pe(e2) {
  var t2;
  return null == (t2 = null == e2 ? oe : e2) ? Promise : t2;
}
function Ee(e2, t2, r2, n2, i2, o2, s2) {
  try {
    var l2 = e2[o2](s2), a2 = l2.value;
  } catch (e3) {
    return void r2(e3);
  }
  l2.done ? t2(a2) : Promise.resolve(a2).then(n2, i2);
}
function Me(e2) {
  return function() {
    var t2 = this, r2 = arguments;
    return new Promise(function(n2, i2) {
      var o2 = e2.apply(t2, r2);
      function s2(e3) {
        Ee(o2, n2, i2, s2, l2, "next", e3);
      }
      function l2(e3) {
        Ee(o2, n2, i2, s2, l2, "throw", e3);
      }
      s2(void 0);
    });
  };
}
var Fe = (e2 = {}) => {
  let { parentOrigin: t2 = "*", timeout: r2, debug: n2 = false, config: i2 = {} } = e2, o2 = /* @__PURE__ */ ((e3) => (...t3) => {
    e3 && console.log("[Penpal]", ...t3);
  })(n2), s2 = /* @__PURE__ */ ((e3, t3) => {
    let r3 = [], n3 = false;
    return { destroy(i3) {
      n3 || (n3 = true, t3(`${e3}: Destroying connection`), r3.forEach((e4) => {
        e4(i3);
      }));
    }, onDestroy(e4) {
      n3 ? e4() : r3.push(e4);
    } };
  })("App", o2), { destroy: l2, onDestroy: a2 } = s2;
  return { promise: new Promise((n3, u2) => {
    let c2 = k(r2, l2), p2 = (r3) => {
      if ((() => {
        try {
          clearTimeout(void 0);
        } catch (g2) {
          return false;
        }
        return true;
      })() && r3.source === parent && r3.data && r3.data.penpal === g.SynAck) {
        let l3 = r3.data, a3 = new J(l3.id, l3.appName), u3 = new J(l3.id + "/api", l3.appName), d2 = l3.id, f2 = {}, h2 = (e3) => "api/" + e3, v2 = { $emit: (e3, ...t3) => {
          a3.getEventCallBackLength(e3) > 0 ? a3.$pureEmit(e3, ...t3) : null == f2 || f2.$off(e3, () => {
          }, true);
        }, onQueryResult: (e3) => {
          u3.$pureEmit(h2(e3.reqid), e3);
        }, onOsNotify: (e3) => {
        } }, y2 = (e3) => function(t3) {
          let r4 = h2(e3);
          u3.$on(r4, function e4(n4) {
            t3.next(n4), n4.reqid && "succ" === n4.result && (t3.complete(), u3.$off(r4, e4));
          });
        }, g2 = $(v2), b2 = q(t2, g2, C(C({}, i2), {}, { postmateVersion: "0.1.5" }), s2, o2, e2.callTimeouts)(r3), m2 = {}, w2 = b2.bus;
        if (w2) {
          let e3 = (e4, t3, r4) => {
            r4 ? (t3.$__once = true, a3.$once(e4, t3)) : a3.$on(e4, t3), m2[e4] ? m2[e4].push(t3) : (m2[e4] = [t3], w2.$on(e4, d2));
          };
          Object.assign(f2, w2, { $on: e3, $off: (e4, t3, r4) => {
            var n4;
            if (m2[e4]) {
              let r5 = m2[e4].indexOf(t3);
              r5 > -1 && (m2[e4].splice(r5, 1), a3.$off(e4, t3));
            }
            r4 && delete m2[e4], null != (n4 = m2[e4]) && n4.length || (w2.$off(e4, d2), delete m2[e4]);
          }, $once: (t3, r4) => {
            e3(t3, r4, true);
          } });
        }
        let S2 = C(C({}, b2), {}, { bus: f2, query: (function() {
          var e3 = Me(function* (e4, t3) {
            let r4 = (null == e4 ? void 0 : e4.reqid) || (yield b2.genReqId());
            return new Promise((n4, i3) => {
              b2.query(C(C({}, e4), {}, { reqid: r4 }), t3).then((e5) => {
                "succ" === (null == e5 ? void 0 : e5.result) ? n4(e5) : i3(e5);
              });
            });
          });
          return function(t3, r4) {
            return e3.apply(this, arguments);
          };
        })(), createObservableQuery: (function() {
          var e3 = Me(function* (e4, t3 = {}) {
            let r4 = (null == e4 ? void 0 : e4.reqid) || (yield b2.genReqId()), n4 = new Oe(y2(r4));
            return b2.query(C(C({}, e4), {}, { reqid: r4 }), C(C({}, t3), {}, { observable: true })), n4;
          });
          return function(t3) {
            return e3.apply(this, arguments);
          };
        })() });
        b2 && (window.removeEventListener(A.Message, p2), c2(), n3({ methods: S2, config: r3.data.config }));
      }
    };
    window.addEventListener(A.Message, p2), (() => {
      o2("Child: Handshake - Sending SYN");
      let e3 = { penpal: g.Syn }, r3 = t2 instanceof RegExp ? "*" : t2;
      window.parent.postMessage(e3, r3);
    })(), a2((e3) => {
      window.removeEventListener(A.Message, p2), e3 && u2(e3);
    });
  }), destroy() {
    l2();
  } };
};
var $e = "trimjs-extension-host";
function _e() {
  return "undefined" != typeof crypto && "function" == typeof crypto.randomUUID ? crypto.randomUUID() : `web-ext-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}
function ke(e2) {
  if (!e2 || "object" != typeof e2) return false;
  const t2 = e2;
  return t2.source === $e && 1 === t2.version && "string" == typeof t2.requestId && ("request" === t2.kind || "response" === t2.kind || "event" === t2.kind);
}
function Te(e2) {
  const t2 = /* @__PURE__ */ new Error(`NotSupportedInExtensionHost: ${e2}`);
  throw t2.code = "NotSupportedInExtensionHost", t2;
}
function je(e2) {
  var t2;
  const r2 = null !== (t2 = null == e2 ? void 0 : e2.timeoutMs) && void 0 !== t2 ? t2 : 1500, n2 = /* @__PURE__ */ new Map(), i2 = /* @__PURE__ */ new Set(), o2 = (e3) => {
    if (ke(t3 = e3.data) && "response" === t3.kind) {
      const t4 = n2.get(e3.data.requestId);
      if (!t4) return;
      return n2.delete(e3.data.requestId), clearTimeout(t4.timeoutId), void t4.resolve(e3.data);
    }
    var t3;
    (function(e4) {
      return ke(e4) && "event" === e4.kind;
    })(e3.data) && i2.forEach((t4) => {
      t4(e3.data);
    });
  };
  return window.addEventListener("message", o2), { request: (e3, t3, i3) => s(function* () {
    const o3 = (function(e4, t4) {
      return { source: $e, version: 1, kind: "request", requestId: _e(), payload: { method: e4, params: t4 } };
    })(e3, t3), s2 = yield new Promise((t4, s3) => {
      let l3;
      const a3 = i3 && "timeoutMs" in i3 ? i3.timeoutMs : r2;
      "number" == typeof a3 && a3 >= 0 && (l3 = setTimeout(() => {
        n2.delete(o3.requestId), s3(/* @__PURE__ */ new Error(`Extension host timeout while calling ${e3}`));
      }, a3)), n2.set(o3.requestId, { resolve: t4, reject: s3, timeoutId: l3 }), window.postMessage(o3, "*");
    });
    var l2, a2;
    if (!s2.payload.ok) throw new Error(null !== (l2 = null === (a2 = s2.payload.error) || void 0 === a2 ? void 0 : a2.message) && void 0 !== l2 ? l2 : `Extension host request failed: ${e3}`);
    return s2.payload.result;
  })(), onEvent: (e3) => (i2.add(e3), () => {
    i2.delete(e3);
  }), destroy() {
    window.removeEventListener("message", o2), n2.forEach((e3) => {
      clearTimeout(e3.timeoutId), e3.reject(/* @__PURE__ */ new Error("Extension bridge destroyed"));
    }), n2.clear(), i2.clear();
  } };
}
function Ne() {
  return (Ne = s(function* (e2) {
    const t2 = je();
    try {
      var r2;
      const i2 = yield t2.request("handshake", { debug: null !== (r2 = null == e2 ? void 0 : e2.debug) && void 0 !== r2 && r2 }, { timeoutMs: 5e3 });
      return n.log("Extension host probe response:", i2), true === i2.available;
    } catch (i2) {
      return n.log("Extension host probe failed:", i2), false;
    } finally {
      t2.destroy();
    }
  })).apply(this, arguments);
}
function We() {
  return We = s(function* () {
    const e2 = je(), t2 = /* @__PURE__ */ new Map();
    e2.onEvent((e3) => {
      const r3 = t2.get(e3.payload.event);
      null == r3 || r3.forEach((t3) => {
        t3(e3.payload.data);
      });
    }), yield e2.request("handshake", {}, { timeoutMs: 5e3 });
    const r2 = yield e2.request("getPlatformConfig", {}), n2 = { bus: { $on(r3, n3) {
      var i3;
      const o3 = null !== (i3 = t2.get(r3)) && void 0 !== i3 ? i3 : /* @__PURE__ */ new Set(), s2 = 0 === o3.size;
      o3.add(n3), t2.set(r3, o3), s2 && e2.request("subscribe", { event: r3 });
    }, $off(r3, n3) {
      const i3 = t2.get(r3);
      i3 && (i3.delete(n3), 0 === i3.size && (t2.delete(r3), e2.request("unsubscribe", { event: r3 })));
    }, $once(e3, t3) {
      const r3 = function() {
        n2.bus.$off(e3, r3), t3(...arguments);
      };
      n2.bus.$on(e3, r3);
    }, $emit() {
      Te("bus.$emit");
    } }, genReqId: () => _e(), query: (t3, r3) => e2.request("query", { params: t3, config: r3 }), createObservableQuery: (o2 = s(function* () {
      return Te("createObservableQuery");
    }), function() {
      return o2.apply(this, arguments);
    }), openApp: () => Te("openApp"), openAppSetting: () => Te("openAppSetting"), openCustomApp: () => Te("openCustomApp"), setTitle: () => Te("setTitle"), pickFile: (t3) => e2.request("pickFile", t3, { timeoutMs: null }), pickUserFile: () => Te("pickUserFile"), pickSharedFile: () => Te("pickSharedFile"), showFileDetails: (e3, t3) => Te("showFileDetails"), authorizeUserFile: (e3) => Te("authorizeUserFile"), authorizeSharedFile: (e3) => Te("authorizeSharedFile"), close: (i2 = s(function* () {
      e2.destroy();
    }), function() {
      return i2.apply(this, arguments);
    }), refreshToken: () => e2.request("refreshToken", {}), setExitPageTips: () => Te("setExitPageTips"), openFileManagerApp: () => Te("openFileManagerApp"), openFileManager: () => Te("openFileManager"), openFile: () => Te("openFile"), getPlatformConfig: () => Promise.resolve(r2), getHostSnapshot: () => e2.request("getHostSnapshot", {}) };
    var i2, o2, l2;
    return { methods: n2, config: (l2 = r2, { appName: "extension-host", os: { version: l2.systemVersion, theme: l2.theme, language: l2.language, format: { date: { date: l2.format.date, time: l2.format.time } } } }) };
  }), We.apply(this, arguments);
}
var Ce = null;
function qe() {
  return (qe = s(function* (e2) {
    const t2 = Fe({ callTimeouts: { getSessionCredentials: 5e3 }, debug: null == e2 ? void 0 : e2.debug, timeout: 1500 });
    try {
      const e3 = yield t2.promise;
      return e3;
    } catch (r2) {
      return t2.destroy(), n.log("Native OS connection probe failed:", r2), null;
    }
  })).apply(this, arguments);
}
var xe = (e2) => Ce || (Ce = s(function* () {
  if ("undefined" != typeof window && window.parent !== window) {
    const t2 = yield (function(e3) {
      return qe.apply(this, arguments);
    })(e2);
    if (t2) return t2;
  }
  return (yield (function(e3) {
    return Ne.apply(this, arguments);
  })(e2)) ? (function() {
    return We.apply(this, arguments);
  })() : Fe({ callTimeouts: { getSessionCredentials: 5e3 }, debug: null == e2 ? void 0 : e2.debug }).promise;
})());
var Ie = { pickFile: "pick-file", pickUserFile: "pick-user-file", pickSharedFile: "pick-shared-file", authorizeUserFile: "authorize-user-file", authorizeSharedFile: "authorize-shared-file" };
var Ue = class {
  constructor() {
    r(this, "options", void 0), r(this, "flutterInAppWebView", null), r(this, "osConnector", null), r(this, "initPromise", void 0), r(this, "appApiVersion", void 0), r(this, "isWeb", true), r(this, "isStandaloneWeb", false);
    let e2 = arguments.length > 0 && void 0 !== arguments[0] ? arguments[0] : {};
    this.options = e2, e2.debug && (n.setDebug(true), n.log("Trim App initialized with debug mode")), n.log("Options:", e2), this.initPromise = this.init();
  }
  init() {
    var e2 = this;
    return s(function* () {
      return n.log("Starting initialization..."), new Promise((t2) => {
        if (i()) e2.isWeb = false, n.log("Initializing mobile platform..."), new Promise((e3) => {
          var t3;
          (c ? window.flutter_inappwebview : u || (null === (t3 = window.flutter_inappwebview) || void 0 === t3 ? void 0 : t3._platformReady)) ? (n.log("flutter_inappwebview is ready"), e3(window.flutter_inappwebview)) : window.addEventListener("flutterInAppWebViewPlatformReady", () => {
            n.log("flutter_inappwebview is ready"), e3(window.flutter_inappwebview);
          });
        }).then((r2 = s(function* (r3) {
          yield e2.initMobileAppApi(r3), n.log("Mobile platform initialized"), t2();
        }), function(e3) {
          return r2.apply(this, arguments);
        }));
        else {
          if (e2.isWeb = true, e2.isStandaloneWeb = "undefined" != typeof window && window.parent === window, e2.isStandaloneWeb) return n.log("Standalone web platform detected; host bridge initialization skipped"), void t2();
          n.log("Initializing web platform..."), xe().then((r3) => {
            e2.osConnector = r3, n.log("Web platform initialized"), t2();
          });
        }
        var r2;
      });
    })();
  }
  getWebMethods() {
    var e2;
    const t2 = null === (e2 = this.osConnector) || void 0 === e2 ? void 0 : e2.methods;
    if (!t2) throw new Error("Host bridge is not available outside iframe or app runtime");
    return t2;
  }
  loadAppMessage() {
    var e2 = this;
    return s(function* () {
      return e2.callAppMethod("getAppMessage");
    })();
  }
  initMobileAppApi(e2) {
    var t2 = this;
    return s(function* () {
      t2.flutterInAppWebView = e2;
      try {
        const e3 = yield t2.loadAppMessage();
        t2.appApiVersion = null == e3 ? void 0 : e3.appApi;
      } catch (r2) {
        n.log("Failed to load mobile appApi version:", r2), t2.appApiVersion = void 0;
      }
    })();
  }
  assertMobileAppApi(e2) {
    let t2 = arguments.length > 1 && void 0 !== arguments[1] ? arguments[1] : "v1.0";
    if (!this.isAtLeastVersion(this.appApiVersion, t2)) throw new Error(`${e2} requires appApi >= ${t2}`);
  }
  isAtLeastVersion(e2, t2) {
    if (!e2) return false;
    const r2 = this.parseVersion(e2), n2 = this.parseVersion(t2), i2 = Math.max(r2.length, n2.length);
    for (let l2 = 0; l2 < i2; l2 += 1) {
      var o2, s2;
      const e3 = null !== (o2 = r2[l2]) && void 0 !== o2 ? o2 : 0, t3 = null !== (s2 = n2[l2]) && void 0 !== s2 ? s2 : 0;
      if (e3 > t3) return true;
      if (e3 < t3) return false;
    }
    return true;
  }
  parseVersion(e2) {
    return e2.replace(/^v/i, "").split(".").map((e3) => Number.parseInt(e3, 10) || 0);
  }
  callAppMethod(e2) {
    var t2 = arguments, r2 = this;
    return s(function* () {
      if (!r2.flutterInAppWebView) return null;
      for (var n2 = t2.length, i2 = new Array(n2 > 1 ? n2 - 1 : 0), o2 = 1; o2 < n2; o2++) i2[o2 - 1] = t2[o2];
      return p(r2.flutterInAppWebView, e2, ...i2);
    })();
  }
  ready() {
    var e2 = this;
    return s(function* () {
      return e2.initPromise;
    })();
  }
  getHostCapabilities() {
    var e2 = this;
    return s(function* () {
      var t2;
      let r2;
      try {
        yield Promise.race([e2.initPromise, new Promise((e3, t3) => {
          r2 = setTimeout(() => t3(/* @__PURE__ */ new Error("Host connection unavailable")), 5e3);
        })]);
      } finally {
        void 0 !== r2 && clearTimeout(r2);
      }
      if (!e2.isWeb) return { runtime: "mobile" };
      const n2 = null === (t2 = e2.osConnector) || void 0 === t2 || null === (t2 = t2.config) || void 0 === t2 ? void 0 : t2.host;
      return "desktop" === (null == n2 ? void 0 : n2.runtime) ? a({ runtime: "desktop" }, 1 === n2.sessionCredentials ? { sessionCredentials: 1 } : {}) : { runtime: "web" === (null == n2 ? void 0 : n2.runtime) ? "web" : "unknown" };
    })();
  }
  getSessionCredentials() {
    var e2 = this;
    return s(function* () {
      const t2 = (e3) => ({ code: 1, msg: e3, error: e3, data: null });
      try {
        var r2, n2;
        const i2 = yield e2.getHostCapabilities(), o2 = null === (r2 = e2.osConnector) || void 0 === r2 ? void 0 : r2.methods.getSessionCredentials;
        if ("desktop" !== i2.runtime || 1 !== i2.sessionCredentials || !o2) return t2("NOT_SUPPORTED");
        const s2 = yield o2();
        return 0 === (null == s2 ? void 0 : s2.code) && "string" == typeof (null === (n2 = s2.data) || void 0 === n2 ? void 0 : n2.secret) && s2.data.secret ? { code: 0, msg: "", data: a({ secret: s2.data.secret }, "string" == typeof s2.data.token && s2.data.token ? { token: s2.data.token } : {}) } : 1 === (null == s2 ? void 0 : s2.code) && ["NOT_SUPPORTED", "FORBIDDEN", "SESSION_UNAVAILABLE", "CONNECTION_CLOSED"].includes(s2.error) ? t2(s2.error) : t2("SESSION_UNAVAILABLE");
      } catch (i2) {
        return t2("CONNECTION_CLOSED");
      }
    })();
  }
  getOptions() {
    return this.options;
  }
  setOptions(e2) {
    this.options = a(a({}, this.options), e2), void 0 !== e2.debug && n.setDebug(e2.debug), n.log("Options updated:", this.options);
  }
  getPlatformConfig() {
    var e2 = this;
    return s(function* () {
      if (yield e2.initPromise, e2.isWeb) {
        const t3 = yield e2.getWebMethods().getPlatformConfig();
        if (!t3) throw new Error("Failed to get platform config");
        return t3;
      }
      if (e2.appApiVersion) {
        const t3 = yield e2.callAppMethod("getPlatformConfig");
        if (!t3) throw new Error("Failed to get platform config");
        return t3;
      }
      const t2 = yield e2.loadAppMessage();
      if (!t2) throw new Error("Failed to get platform config");
      return e2.appApiVersion = t2.appApi, { theme: t2.nightMode, language: t2.language, appVersion: t2.appVersion, systemVersion: t2.systemVersion, format: { date: t2.dateFormat, time: t2.timeFormat } };
    })();
  }
  getHostSnapshot() {
    var e2 = this;
    return s(function* () {
      if (yield e2.initPromise, !e2.isWeb) throw new Error("getHostSnapshot is not supported on mobile platform");
      const t2 = yield e2.getWebMethods().getHostSnapshot();
      if (!t2) throw new Error("Failed to get host snapshot");
      return t2;
    })();
  }
  getAppAuthBaseUrl() {
    var e2 = this;
    return s(function* () {
      if ("undefined" != typeof window && window.parent !== window) {
        const e3 = document.referrer;
        if (e3) try {
          return new URL(e3).origin;
        } catch (t2) {
          n.log("Failed to derive app auth base url from document.referrer:", t2);
        }
      }
      if ("undefined" != typeof window) return window.location.origin;
      if (yield e2.initPromise, e2.isWeb) try {
        const t2 = yield e2.getHostSnapshot();
        if (t2.host) return t2.host;
      } catch (t2) {
        n.log("Failed to derive app auth host snapshot:", t2);
      }
      throw new Error("Failed to derive app auth base url");
    })();
  }
  buildAppAuthUrl(e2, t2) {
    var r2 = this;
    return s(function* () {
      return (function(e3, t3, r3, n2) {
        var i2, o2;
        const s2 = new URL(e3, null !== (i2 = null != n2 ? n2 : null === (o2 = globalThis.location) || void 0 === o2 ? void 0 : o2.origin) && void 0 !== i2 ? i2 : "http://localhost"), l2 = Ie[t3];
        if (s2.pathname = (function(e4) {
          return `/app-auth/${e4}`;
        })(l2), s2.searchParams.set("appName", r3.appName), r3.redirectUri && s2.searchParams.set("redirectUri", r3.redirectUri), r3.state && s2.searchParams.set("state", r3.state), "pickFile" === t3 || "pickUserFile" === t3 || "pickSharedFile" === t3) {
          var a2;
          const e4 = r3;
          "pickSharedFile" !== t3 && e4.directory && s2.searchParams.set("directory", "true");
          const n3 = "pickSharedFile" !== t3 ? e4.accept : void 0;
          (null == n3 ? void 0 : n3.length) && s2.searchParams.set("accept", n3.join(",")), (null === (a2 = e4.sidebarGroup) || void 0 === a2 ? void 0 : a2.length) && s2.searchParams.set("sidebarGroup", e4.sidebarGroup.join(","));
        } else s2.searchParams.set("path", r3.path);
        return s2.toString();
      })(yield r2.getAppAuthBaseUrl(), e2, t2, window.location.origin);
    })();
  }
  openAppAuth(e2, t2, r2) {
    var n2 = this;
    return s(function* () {
      if (yield n2.initPromise, !n2.isWeb) throw new Error("openAppAuth is not supported on mobile platform");
      const i2 = yield n2.buildAppAuthUrl(e2, t2);
      return "_self" === (null == r2 ? void 0 : r2.target) && "undefined" != typeof window ? (window.location.assign(i2), i2) : (yield n2.openURL(i2, null == r2 ? void 0 : r2.target, null == r2 ? void 0 : r2.features), i2);
    })();
  }
  parseAppAuthCallback(e2) {
    return (function(e3) {
      var t2, r2, n2, i2;
      const o2 = e3 instanceof URLSearchParams ? e3 : e3 instanceof URL ? e3.searchParams : "string" == typeof e3 ? new URL(e3, null !== (t2 = null === (r2 = globalThis.location) || void 0 === r2 ? void 0 : r2.origin) && void 0 !== t2 ? t2 : "http://localhost").searchParams : new URLSearchParams(null !== (n2 = null === (i2 = globalThis.location) || void 0 === i2 ? void 0 : i2.search) && void 0 !== n2 ? n2 : ""), s2 = o2.get("method"), l2 = s2 && Object.prototype.hasOwnProperty.call(Ie, s2) ? s2 : void 0, a2 = o2.get("path");
      let u2;
      if (a2) try {
        const e4 = JSON.parse(a2);
        Array.isArray(e4) && e4.every((e5) => "string" == typeof e5) && (u2 = e4);
      } catch (c2) {
        u2 = void 0;
      }
      return { status: o2.get("status") || void 0, error: o2.get("error") || void 0, method: l2, appName: o2.get("appName") || void 0, state: o2.get("state") || void 0, path: u2 };
    })(e2);
  }
  setTitle(e2) {
    var t2 = this;
    return s(function* () {
      if (yield t2.initPromise, n.log("Setting title:", e2), document.title = e2, t2.isWeb) return t2.getWebMethods().setTitle(e2);
    })();
  }
  setExitPageTips(e2) {
    var t2 = this;
    return s(function* () {
      return yield t2.initPromise, t2.isWeb ? t2.getWebMethods().setExitPageTips(e2) : t2.callAppMethod("setExitPageTips", e2 ? JSON.stringify(e2) : "");
    })();
  }
  openFile(e2) {
    var t2 = this;
    return s(function* () {
      return yield t2.initPromise, n.log("Opening file:", e2), t2.isWeb ? t2.getWebMethods().openFile(e2) : t2.callAppMethod("openFile", e2);
    })();
  }
  openFileManager(e2) {
    var t2 = this;
    return s(function* () {
      if (yield t2.initPromise, n.log("Opening file manager:", e2), t2.isWeb) {
        const r2 = t2.getWebMethods();
        return (r2.openFileManager || r2.openFileManagerApp)(e2);
      }
      return t2.assertMobileAppApi("openFileManager"), t2.callAppMethod("openFileManager", e2);
    })();
  }
  showFileDetails(e2, t2) {
    var r2 = this;
    return s(function* () {
      return yield r2.initPromise, r2.isWeb ? r2.getWebMethods().showFileDetails(e2, t2) : (r2.assertMobileAppApi("showFileDetails"), r2.callAppMethod("showFileDetails", JSON.stringify({ paths: e2, options: t2 })));
    })();
  }
  authorizeUserFile(e2) {
    var t2 = this;
    return s(function* () {
      var r2;
      return yield t2.initPromise, t2.isWeb ? t2.getWebMethods().authorizeUserFile(e2) : (t2.assertMobileAppApi("authorizeUserFile"), null !== (r2 = yield t2.callAppMethod("authorizeUserFile", JSON.stringify({ path: e2 }))) && void 0 !== r2 ? r2 : void 0);
    })();
  }
  authorizeSharedFile(e2) {
    var t2 = this;
    return s(function* () {
      var r2;
      return yield t2.initPromise, t2.isWeb ? t2.getWebMethods().authorizeSharedFile(e2) : (t2.assertMobileAppApi("authorizeSharedFile"), null !== (r2 = yield t2.callAppMethod("authorizeSharedFile", JSON.stringify({ path: e2 }))) && void 0 !== r2 ? r2 : void 0);
    })();
  }
  openAppSetting() {
    var e2 = this;
    return s(function* () {
      return yield e2.initPromise, n.log("Opening app setting"), e2.isWeb ? e2.getWebMethods().openAppSetting() : (e2.assertMobileAppApi("openAppSetting"), e2.callAppMethod("openAppSetting"));
    })();
  }
  openApp(e2) {
    var t2 = this;
    return s(function* () {
      return yield t2.initPromise, n.log("Opening app:", e2), t2.isWeb ? t2.getWebMethods().openApp(e2) : !!i() && t2.callAppMethod("openAppPage", e2);
    })();
  }
  openCustomApp(e2, t2) {
    var r2 = this;
    return s(function* () {
      if (yield r2.initPromise, n.log("Opening custom app:", e2, t2), r2.isWeb) return r2.getWebMethods().openCustomApp(e2, t2);
      throw new Error("openCustomApp is not supported on mobile platform");
    })();
  }
  openURL(e2, t2, r2) {
    var i2 = this;
    return s(function* () {
      if (yield i2.initPromise, n.log("Opening URL:", e2, t2, r2), !i2.isWeb) return i2.callAppMethod("openSystemBrowser", e2);
      if ("_self" === t2) return void window.location.assign(e2);
      if (!r2) return void window.open(e2, t2, r2);
      const o2 = window.open("", t2, r2);
      o2 && (o2.location.href = e2);
    })();
  }
  close() {
    var e2 = this;
    return s(function* () {
      return yield e2.initPromise, e2.isWeb ? e2.getWebMethods().close() : e2.callAppMethod("exitPage");
    })();
  }
  query(e2, t2) {
    var r2 = this;
    return s(function* () {
      if (yield r2.initPromise, r2.isWeb) {
        if (null == t2 ? void 0 : t2.observable) return r2.getWebMethods().createObservableQuery(e2, t2);
        const n3 = yield r2.getWebMethods().query(e2, t2);
        if (!n3) throw new Error("Failed to query");
        return n3;
      }
      if (r2.assertMobileAppApi("query"), null == t2 ? void 0 : t2.observable) {
        if (!r2.flutterInAppWebView) throw new Error("Failed to start observable query");
        return (function(e3, t3, r3) {
          return v(), new f((n3) => {
            let i2, o2 = false;
            return p(e3, "wsQuery", null == t3 ? void 0 : t3.req, JSON.stringify(t3), void 0 === r3 ? void 0 : JSON.stringify(r3)).then((e4) => {
              (null == e4 ? void 0 : e4.reqId) ? (i2 = e4.reqId, o2 || h.set(i2, n3)) : n3.error(/* @__PURE__ */ new Error("Failed to start observable query"));
            }, (e4) => {
              n3.error(e4);
            }), () => {
              o2 = true, i2 && h.delete(i2);
            };
          });
        })(r2.flutterInAppWebView, e2, t2);
      }
      const n2 = yield r2.callAppMethod("wsQuery", null == e2 ? void 0 : e2.req, JSON.stringify(e2), void 0 === t2 ? void 0 : JSON.stringify(t2));
      if (!n2) throw new Error("Failed to query");
      if ("succ" !== n2.result) throw n2;
      return n2;
    })();
  }
  refreshToken() {
    var e2 = this;
    return s(function* () {
      if (yield e2.initPromise, !e2.isWeb) throw new Error("refreshToken is not supported on mobile platform");
      return e2.getWebMethods().refreshToken();
    })();
  }
  $on(e2, t2) {
    var r2 = this;
    return s(function* () {
      if (yield r2.initPromise, !r2.isWeb) throw new Error("$on is not supported on mobile platform");
      const n2 = r2.getWebMethods().bus;
      if (!n2) throw new Error("bus is not available");
      n2.$on(e2, t2);
    })();
  }
  $off(e2, t2) {
    var r2 = this;
    return s(function* () {
      if (yield r2.initPromise, !r2.isWeb) throw new Error("$off is not supported on mobile platform");
      const n2 = r2.getWebMethods().bus;
      if (!n2) throw new Error("bus is not available");
      n2.$off(e2, t2);
    })();
  }
  $once(e2, t2) {
    var r2 = this;
    return s(function* () {
      if (yield r2.initPromise, !r2.isWeb) throw new Error("$once is not supported on mobile platform");
      const n2 = r2.getWebMethods().bus;
      if (!n2) throw new Error("bus is not available");
      n2.$once(e2, t2);
    })();
  }
  pickFile(e2) {
    var t2 = this;
    return s(function* () {
      var r2, n2;
      return yield t2.initPromise, t2.isWeb ? null === (r2 = t2.osConnector) || void 0 === r2 ? void 0 : r2.methods.pickFile(e2) : t2.isAtLeastVersion(t2.appApiVersion, "v1.0") ? null !== (n2 = yield t2.callAppMethod("pickFile", JSON.stringify(e2))) && void 0 !== n2 ? n2 : void 0 : t2.flutterInAppWebView ? (function(e3, t3) {
        return y.apply(this, arguments);
      })(t2.flutterInAppWebView, e2) : void 0;
    })();
  }
  pickUserFile() {
    var e2 = arguments, t2 = this;
    return s(function* () {
      var r2;
      let n2 = e2.length > 0 && void 0 !== e2[0] ? e2[0] : {};
      return yield t2.initPromise, t2.isWeb ? t2.getWebMethods().pickUserFile(n2) : (t2.assertMobileAppApi("pickUserFile"), null !== (r2 = yield t2.callAppMethod("pickUserFile", JSON.stringify(n2))) && void 0 !== r2 ? r2 : void 0);
    })();
  }
  pickSharedFile() {
    var e2 = arguments, t2 = this;
    return s(function* () {
      var r2;
      let n2 = e2.length > 0 && void 0 !== e2[0] ? e2[0] : {};
      return yield t2.initPromise, t2.isWeb ? t2.getWebMethods().pickSharedFile(n2) : (t2.assertMobileAppApi("pickSharedFile"), null !== (r2 = yield t2.callAppMethod("pickSharedFile", JSON.stringify(n2))) && void 0 !== r2 ? r2 : void 0);
    })();
  }
};

// src/client/index.js
var LOG_PREFIX = "[dsh-fnos-advanced]";
var VERSION = "0.2.3";
var MSG_TYPE = "dsh-fnos-advanced:auth";
var STORAGE_KEY = "dsh-fnos-advanced:auth-result";
var AUTH_BASE_KEY = "dsh-fnos-advanced:auth-base";
var SETTINGS_SLOT_KEY = "dsh-fnos-advanced";
var DEFAULT_SETTINGS = Object.freeze({ proxyBase: "", lanTarget: "5666", authMode: "ask" });
var BUTTON_ATTR = "data-dsh-fnos-advanced";
var STATUS_CLASS = "dsh-fnos-advanced-status";
var STYLE_ID = "dsh-fnos-advanced-style";
var WORKSPACE_TITLE_RE = /workspace|工作区/i;
var PATH_LABEL_RE = /(?:^|\b)(?:edit\s*path|path)(?:\b|$)|路径|编辑路径|修改路径/i;
var OPEN_BUTTON_RE = /^(?:open|打开|确定|选择|select|choose)/i;
var CANCEL_BUTTON_RE = /^(?:cancel|取消)/i;
var POPUP_FEATURES = "popup=yes,width=760,height=680,resizable=yes,scrollbars=yes";
var AUTH_TIMEOUT_MS = 5 * 60 * 1e3;
var FOLDER_OPEN_SVG = '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" stroke-width="1"><path d="M1.5 3.5C1.5 2.95 1.95 2.5 2.5 2.5H5.4C5.65 2.5 5.88 2.59 6.07 2.75L7.7 4.2C7.88 4.36 8.12 4.45 8.36 4.45H13.5C14.05 4.45 14.5 4.9 14.5 5.45V12.5C14.5 13.05 14.05 13.5 13.5 13.5H2.5C1.95 13.5 1.5 13.05 1.5 12.5V3.5Z" stroke="currentColor"/></svg>';
var installedDispose = null;
var sdkInstance = null;
var configPromise = null;
var settingsCache = null;
var settingsCachedAt = 0;
var settingsPromise = null;
var settingsHostState = "unknown";
var workspacePaths = /* @__PURE__ */ new Map();
var lastWorkspaceMenu = null;
function apply(ctx) {
  if (typeof window === "undefined" || typeof document === "undefined") return;
  const dispose = install();
  if (ctx && typeof ctx.effect === "function") {
    ctx.effect(() => dispose, "dsh-fnos-advanced: workspace directory picker");
  }
  registerSettingsSlot(ctx);
  registerWorkspacePaths(ctx);
}
function install() {
  if (installedDispose) return installedDispose;
  injectStyle();
  const cleanups = [];
  let scanTimer = 0;
  const scheduleScan = () => {
    if (scanTimer) return;
    scanTimer = window.setTimeout(() => {
      scanTimer = 0;
      scan();
    }, 80);
  };
  const observer = new MutationObserver(scheduleScan);
  observer.observe(document.documentElement, { childList: true, subtree: true });
  cleanups.push(() => observer.disconnect());
  const onClick = (event) => {
    const target = event.target instanceof Element ? event.target.closest(`[${BUTTON_ATTR}]`) : null;
    if (!target) return;
    event.preventDefault();
    event.stopPropagation();
    void handlePick(target);
  };
  document.addEventListener("click", onClick, true);
  cleanups.push(() => document.removeEventListener("click", onClick, true));
  const onWorkspaceRowClick = (event) => {
    const row = event.target instanceof Element ? event.target.closest('[data-row-key^="workspace:"]') : null;
    if (!row) return;
    const key = row.getAttribute("data-row-key") || "";
    const id = key.slice("workspace:".length);
    if (id) {
      lastWorkspaceMenu = { id, at: Date.now() };
      scheduleScan();
    }
  };
  document.addEventListener("click", onWorkspaceRowClick, true);
  cleanups.push(() => document.removeEventListener("click", onWorkspaceRowClick, true));
  getSdk().ready().then(scheduleScan).catch(() => {
  });
  const dispose = () => {
    if (scanTimer) {
      window.clearTimeout(scanTimer);
      scanTimer = 0;
    }
    for (const cleanup of cleanups.splice(0)) {
      try {
        cleanup();
      } catch {
      }
    }
    for (const node of document.querySelectorAll(`[${BUTTON_ATTR}], .${STATUS_CLASS}, [data-dsh-fnos-open-dir]`)) {
      node.remove();
    }
    const style = document.getElementById(STYLE_ID);
    if (style) style.remove();
    installedDispose = null;
  };
  installedDispose = dispose;
  scan();
  return dispose;
}
function scan() {
  for (const dialog of document.querySelectorAll('[role="dialog"]')) {
    if (!isWorkspacePicker(dialog)) {
      dialog.querySelectorAll(`[${BUTTON_ATTR}], .${STATUS_CLASS}`).forEach((node) => node.remove());
      continue;
    }
    if (dialog.querySelector(`[${BUTTON_ATTR}]`)) continue;
    injectButton(dialog);
  }
  scanWorkspaceMenus();
}
function registerWorkspacePaths(ctx) {
  if (!ctx || typeof ctx.inject !== "function") return;
  ctx.inject(["workspaces"], (scope) => {
    const workspaces = scope.workspaces;
    const list = workspaces && workspaces.list;
    if (!list || typeof list.getSnapshot !== "function") return;
    const update = () => {
      try {
        const snapshot = list.getSnapshot();
        const items = Array.isArray(snapshot?.items) ? snapshot.items : [];
        workspacePaths = new Map(
          items.filter((item) => item && item.workspaceId && typeof item.path === "string").map((item) => [String(item.workspaceId), item.path])
        );
      } catch {
      }
    };
    update();
    if (typeof list.subscribe === "function" && typeof scope.effect === "function") {
      try {
        scope.effect(() => list.subscribe(update), "dsh-fnos-advanced: workspace paths");
      } catch {
      }
    }
  });
}
function isFnosHost() {
  const sdk = getSdk();
  if (sdk.isWeb && sdk.isStandaloneWeb) return false;
  return window.parent !== window || sdk.isWeb === false;
}
function scanWorkspaceMenus() {
  if (!isFnosHost()) return;
  if (!lastWorkspaceMenu || Date.now() - lastWorkspaceMenu.at > 5 * 60 * 1e3) return;
  for (const menu of document.querySelectorAll('[role="menu"]')) {
    if (!isVisible(menu)) continue;
    if (menu.querySelector("[data-dsh-fnos-open-dir]")) continue;
    if (!isWorkspaceActionsMenu(menu)) continue;
    injectOpenDirItem(menu);
  }
}
function isWorkspaceActionsMenu(menu) {
  const text = (menu.textContent || "").replace(/\s+/g, " ");
  return /Delete workspace|删除工作区|删除工作空间|删除工作區/i.test(text);
}
function injectOpenDirItem(menu) {
  const viewport = menu.querySelector('[role="presentation"]') || menu;
  const templateButton = menu.querySelector('button[role="menuitem"]');
  const template = templateButton && templateButton.closest("div");
  if (!template) return;
  const item = template.cloneNode(true);
  item.setAttribute("data-dsh-fnos-open-dir", "1");
  const button = item.querySelector('button[role="menuitem"]');
  if (!button) return;
  const spans = Array.from(button.querySelectorAll("span"));
  const label = spans.find((span) => /Rename|重命名|Delete|删除/i.test(span.textContent || "")) || spans[spans.length - 1];
  if (label) label.textContent = "打开目录";
  const icon = spans.find((span) => span !== label);
  if (icon) icon.innerHTML = FOLDER_OPEN_SVG;
  button.addEventListener("click", (event) => {
    event.preventDefault();
    event.stopPropagation();
    const context = lastWorkspaceMenu;
    closeMenu(menu);
    if (!context) {
      showToast("无法确定工作区");
      return;
    }
    void openWorkspaceDirectory(context.id);
  });
  viewport.insertBefore(item, viewport.firstChild);
}
function closeMenu(menu) {
  try {
    const options = { key: "Escape", code: "Escape", keyCode: 27, which: 27, bubbles: true };
    document.dispatchEvent(new KeyboardEvent("keydown", options));
    window.dispatchEvent(new KeyboardEvent("keydown", options));
  } catch {
  }
  window.setTimeout(() => {
    if (menu.isConnected) menu.remove();
  }, 0);
}
async function resolveWorkspacePath(id) {
  const cached = workspacePaths.get(id);
  if (cached) return cached;
  try {
    const response = await fetch(
      `${pluginURL("fnos-advanced/workspace-path")}?id=${encodeURIComponent(id)}`,
      { credentials: "same-origin", cache: "no-store", headers: { Accept: "application/json" } }
    );
    if (response.ok) {
      const data = await response.json();
      if (data && data.ok && typeof data.path === "string" && data.path) {
        workspacePaths.set(id, data.path);
        return data.path;
      }
    }
  } catch {
  }
  return "";
}
async function openWorkspaceDirectory(id) {
  const path = await resolveWorkspacePath(id);
  if (!path) {
    showToast("无法获取工作区目录");
    return;
  }
  const sdk = getSdk();
  try {
    await sdk.ready();
  } catch {
  }
  try {
    await sdk.openFileManager(path);
  } catch (error) {
    console.warn(LOG_PREFIX, "openFileManager failed", error);
    showToast("打开目录失败，当前宿主可能不支持该操作。");
  }
}
function showToast(text) {
  let toast = document.querySelector(".dsh-fnos-advanced-toast");
  if (!toast) {
    toast = document.createElement("div");
    toast.className = "dsh-fnos-advanced-toast";
    toast.setAttribute("role", "status");
    document.body.appendChild(toast);
  }
  toast.textContent = text;
  toast.style.display = "block";
  if (toast.__dshFnosTimer) window.clearTimeout(toast.__dshFnosTimer);
  toast.__dshFnosTimer = window.setTimeout(() => {
    toast.style.display = "none";
  }, 5e3);
}
function isWorkspacePicker(dialog) {
  if (!isVisible(dialog)) return false;
  const title = [
    dialog.getAttribute("aria-label") || "",
    ...Array.from(dialog.querySelectorAll("h1, h2, h3")).map((node) => node.textContent || "")
  ].join(" ");
  if (!WORKSPACE_TITLE_RE.test(title)) return false;
  if (!findPathInput(dialog) && !findEditButton(dialog)) return false;
  return Boolean(findFooter(dialog));
}
function injectButton(dialog) {
  const footer = findFooter(dialog);
  if (!footer) return;
  const button = document.createElement("button");
  button.type = "button";
  button.setAttribute(BUTTON_ATTR, "1");
  button.className = "dsh-fnos-advanced-button";
  button.textContent = "选择飞牛目录";
  button.title = "通过飞牛授权选择一个目录（会授予当前飞牛应用访问权限）";
  footer.insertBefore(button, footer.firstChild);
}
function findPathInput(dialog) {
  for (const input of dialog.querySelectorAll("input")) {
    if (input.type && input.type !== "text") continue;
    const label = [
      input.getAttribute("aria-label") || "",
      input.getAttribute("placeholder") || "",
      input.title || ""
    ].join(" ");
    if (PATH_LABEL_RE.test(label)) return input;
    if (typeof input.value === "string" && input.value.startsWith("/")) return input;
  }
  return null;
}
function findEditButton(dialog) {
  for (const button of dialog.querySelectorAll("button")) {
    const label = [
      button.getAttribute("aria-label") || "",
      button.title || "",
      button.textContent || ""
    ].join(" ");
    if (PATH_LABEL_RE.test(label)) return button;
  }
  const trail = dialog.querySelector('[role="navigation"]');
  const bar = trail && trail.parentElement;
  if (bar) {
    for (const button of bar.querySelectorAll("button")) {
      if (trail.contains(button)) continue;
      if (!(button.textContent || "").trim()) return button;
    }
  }
  return null;
}
function findFooter(dialog) {
  const buttons = Array.from(dialog.querySelectorAll("button"));
  const cancel = buttons.find((button) => CANCEL_BUTTON_RE.test(buttonText(button)));
  const open = buttons.find((button) => OPEN_BUTTON_RE.test(buttonText(button)));
  if (cancel && open && cancel.parentElement === open.parentElement) {
    return cancel.parentElement;
  }
  if (open && open.parentElement) return open.parentElement;
  if (cancel && cancel.parentElement) return cancel.parentElement;
  const last = buttons[buttons.length - 1];
  if (last && last.parentElement && last.parentElement !== dialog) return last.parentElement;
  return null;
}
function buttonText(button) {
  return (button.textContent || "").replace(/\s+/g, " ").trim();
}
async function ensurePathInput(dialog) {
  let input = findPathInput(dialog);
  if (input) return input;
  const edit = findEditButton(dialog);
  if (!edit) return null;
  edit.click();
  for (let i2 = 0; i2 < 24; i2 += 1) {
    await sleep(50);
    input = findPathInput(dialog);
    if (input) {
      input.focus();
      return input;
    }
  }
  return null;
}
async function handlePick(button) {
  if (button.dataset.busy === "1") return;
  const dialog = button.closest('[role="dialog"]');
  if (!dialog) return;
  const originalText = button.textContent || "选择飞牛目录";
  button.dataset.busy = "1";
  button.disabled = true;
  button.textContent = "准备授权…";
  const initialSdk = getSdk();
  const hostLike = window.parent !== window || initialSdk.isWeb === false;
  let popup = null;
  if (!hostLike) {
    popup = window.open("", `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
    if (popup) writePopupLoading(popup);
  }
  try {
    const direct = await tryDirectPick(button, dialog, originalText);
    if (direct) {
      if (popup) {
        try {
          popup.close();
        } catch {
        }
      }
      return;
    }
    const config = await loadConfig();
    const appName = config.appName || "Harness";
    const settings = await loadSettings();
    const target = await resolveAuthTargets(config, settings);
    if (target.mode === "choose") {
      if (popup) {
        try {
          popup.close();
        } catch {
        }
      }
      showAuthChooser(dialog, button, originalText, appName, target.choices);
      return;
    }
    if (!popup || popup.closed) {
      popup = window.open("", `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
    }
    if (!popup) throw new Error("浏览器拦截了授权窗口，请允许弹窗后重试。");
    await startAuthFlow(button, dialog, originalText, appName, target.base, popup);
  } catch (error) {
    if (popup) {
      try {
        popup.close();
      } catch {
      }
    }
    handlePickError(button, dialog, originalText, error);
  }
}
async function tryDirectPick(button, dialog, originalText) {
  const sdk = getSdk();
  try {
    await sdk.ready();
  } catch {
    return false;
  }
  if (sdk.isWeb && sdk.isStandaloneWeb) return false;
  if (sdk.isWeb && window.parent === window) return false;
  button.textContent = "等待选择目录…";
  let result;
  try {
    result = await sdk.pickUserFile({
      directory: true,
      title: "选择工作区目录",
      okText: "确认授权",
      sidebarGroup: ["myFiles", "otherShare", "external", "remote", "favorites", "team"]
    });
  } catch (error) {
    console.warn(LOG_PREFIX, "in-host picker unavailable; falling back to URL auth", error);
    showStatus(dialog, "当前宿主未提供内嵌目录选择器，已改用网页授权。", false);
    return false;
  }
  const path = result && result.code === 0 && Array.isArray(result.data) ? result.data[0] : "";
  if (!path) {
    showStatus(dialog, result && result.msg ? result.msg : "授权已取消", true);
    resetButton(button, originalText);
    return true;
  }
  const input = await ensurePathInput(dialog);
  if (!input) throw new Error("没有找到目录选择器的路径输入框");
  fillAndEnter(input, path);
  showStatus(dialog, `已填入授权目录：${path}`, false);
  button.textContent = "已填入路径";
  window.setTimeout(() => resetButton(button, originalText), 1600);
  return true;
}
async function startAuthFlow(button, dialog, originalText, appName, base, existingPopup) {
  button.textContent = "等待授权…";
  const state = randomId();
  let popup = existingPopup;
  if (!popup || popup.closed) {
    popup = window.open("", `dsh-fnos-advanced-auth-${state}`, POPUP_FEATURES);
  }
  if (!popup) throw new Error("浏览器拦截了授权窗口，请允许弹窗后重试。");
  writePopupLoading(popup);
  try {
    const input = await ensurePathInput(dialog);
    if (!input) throw new Error("没有找到目录选择器的路径输入框");
    const auth = await requestAuth(popup, state, appName, base);
    if (!auth || auth.status !== "success") {
      throw new Error(auth && auth.error ? auth.error : "授权已取消");
    }
    const path = Array.isArray(auth.path) ? auth.path[0] : "";
    if (!path) throw new Error("授权结果里没有目录路径");
    fillAndEnter(input, path);
    showStatus(dialog, `已填入授权目录：${path}`, false);
    button.textContent = "已填入路径";
    window.setTimeout(() => resetButton(button, originalText), 1600);
  } catch (error) {
    try {
      popup.close();
    } catch {
    }
    throw error;
  }
}
function handlePickError(button, dialog, originalText, error) {
  const message = error && error.message ? error.message : String(error);
  console.warn(LOG_PREFIX, message, error);
  showStatus(dialog, message, true);
  resetButton(button, originalText);
}
function resetButton(button, text) {
  button.textContent = text;
  button.disabled = false;
  delete button.dataset.busy;
}
function writePopupLoading(popup) {
  try {
    popup.document.title = "飞牛目录授权";
    popup.document.body.innerHTML = `<p style="font:14px/1.6 -apple-system,BlinkMacSystemFont,'PingFang SC',sans-serif;padding:24px;color:#555">正在打开飞牛授权页面…</p>`;
  } catch {
  }
}
function requestAuth(popup, state, appName, base) {
  return new Promise((resolve, reject) => {
    let settled = false;
    let closeTimer = 0;
    let pollTimer = 0;
    let resultPollTimer = 0;
    let resultPolling = false;
    let timeoutTimer = 0;
    const lookupStored = () => acceptStoredResult(state, accept);
    const cleanup = () => {
      window.removeEventListener("message", onMessage);
      window.removeEventListener("storage", onStorage);
      window.removeEventListener("focus", lookupStored);
      document.removeEventListener("visibilitychange", onVisibility);
      window.clearInterval(pollTimer);
      window.clearInterval(resultPollTimer);
      window.clearTimeout(closeTimer);
      window.clearTimeout(timeoutTimer);
      try {
        localStorage.removeItem(STORAGE_KEY);
      } catch {
      }
    };
    const finish = (result) => {
      if (settled) return;
      settled = true;
      cleanup();
      resolve(result);
    };
    const fail = (error) => {
      if (settled) return;
      settled = true;
      cleanup();
      reject(error);
    };
    const accept = (payload) => {
      if (!payload || settled) return;
      const payloadState = payload.state || payload.result && payload.result.state || "";
      if (state && payloadState && payloadState !== state) return;
      const result = parseAuthPayload(payload);
      if (!result) return;
      finish(result);
    };
    function onMessage(event) {
      if (event.origin !== window.location.origin) return;
      const data = event.data;
      if (!data || data.type !== MSG_TYPE) return;
      accept(data);
    }
    function onStorage(event) {
      if (event.key !== STORAGE_KEY || !event.newValue) return;
      try {
        accept(JSON.parse(event.newValue));
      } catch {
      }
    }
    function onVisibility() {
      if (document.visibilityState === "visible") lookupStored();
    }
    async function readServerResult() {
      try {
        const response = await fetch(
          `${pluginURL("fnos-advanced/auth-result")}?state=${encodeURIComponent(state)}`,
          { credentials: "same-origin", cache: "no-store", headers: { Accept: "application/json" } }
        );
        if (!response.ok) return;
        const data = await response.json();
        if (data && data.ok && typeof data.search === "string") {
          accept({ state: data.state || state, search: data.search });
        }
      } catch {
      }
    }
    async function pollServerResult() {
      if (settled || resultPolling) return;
      resultPolling = true;
      try {
        await readServerResult();
      } finally {
        resultPolling = false;
      }
    }
    window.addEventListener("message", onMessage);
    window.addEventListener("storage", onStorage);
    window.addEventListener("focus", lookupStored);
    document.addEventListener("visibilitychange", onVisibility);
    acceptStoredResult(state, accept);
    resultPollTimer = window.setInterval(pollServerResult, 900);
    pollTimer = window.setInterval(() => {
      if (!popup.closed) return;
      window.clearInterval(pollTimer);
      closeTimer = window.setTimeout(async () => {
        const stored = readStoredResult();
        if (stored) {
          accept(stored);
          return;
        }
        await readServerResult();
        if (!settled) finish({ status: "cancelled", error: "授权窗口已关闭" });
      }, 450);
    }, 500);
    timeoutTimer = window.setTimeout(() => {
      fail(new Error("授权超时，请重试"));
    }, AUTH_TIMEOUT_MS);
    (async () => {
      try {
        const sdk = getSdk();
        const previousBase = sdk.getAppAuthBaseUrl;
        sdk.getAppAuthBaseUrl = async () => base;
        let url;
        try {
          url = await sdk.buildAppAuthUrl("pickUserFile", {
            appName,
            directory: true,
            sidebarGroup: ["myFiles", "otherShare", "external", "remote", "favorites", "team"],
            title: "选择工作区目录",
            okText: "确认授权",
            redirectUri: pluginURL("fnos-advanced/auth-callback.html"),
            state
          });
        } finally {
          sdk.getAppAuthBaseUrl = previousBase;
        }
        if (settled) return;
        popup.location.replace(url);
      } catch (error) {
        fail(error instanceof Error ? error : new Error(String(error)));
      }
    })();
  });
}
function showAuthChooser(dialog, button, originalText, appName, choices) {
  removeChooser(dialog);
  if (getComputedStyle(dialog).position === "static") dialog.style.position = "relative";
  button.textContent = "选择授权入口…";
  const panel = document.createElement("div");
  panel.className = "dsh-fnos-advanced-chooser";
  panel.setAttribute("role", "menu");
  panel.setAttribute("aria-label", "选择授权入口");
  const title = document.createElement("div");
  title.className = "dsh-fnos-advanced-chooser-title";
  title.textContent = "选择授权入口";
  panel.appendChild(title);
  for (const choice of choices) {
    const option = document.createElement("button");
    option.type = "button";
    option.className = "dsh-fnos-advanced-chooser-option";
    option.setAttribute("role", "menuitem");
    const label = document.createElement("span");
    label.className = "dsh-fnos-advanced-chooser-label";
    label.textContent = choice.label;
    const detail = document.createElement("span");
    detail.className = "dsh-fnos-advanced-chooser-detail";
    detail.textContent = choice.detail;
    option.append(label, detail);
    option.addEventListener("click", (event) => {
      event.preventDefault();
      event.stopPropagation();
      removeChooser(dialog);
      startAuthFlow(button, dialog, originalText, appName, choice.base, null).catch((error) => {
        handlePickError(button, dialog, originalText, error);
      });
    });
    panel.appendChild(option);
  }
  const cancel = document.createElement("button");
  cancel.type = "button";
  cancel.className = "dsh-fnos-advanced-chooser-cancel";
  cancel.textContent = "取消";
  cancel.addEventListener("click", (event) => {
    event.preventDefault();
    event.stopPropagation();
    removeChooser(dialog);
    resetButton(button, originalText);
  });
  panel.appendChild(cancel);
  dialog.appendChild(panel);
}
function removeChooser(dialog) {
  dialog.querySelectorAll(".dsh-fnos-advanced-chooser").forEach((node) => node.remove());
}
function parseAuthPayload(payload) {
  if (!payload) return null;
  if (typeof payload.search === "string" && payload.search) {
    try {
      const parsed = getSdk().parseAppAuthCallback(payload.search);
      if (parsed && (parsed.status || parsed.error || parsed.path && parsed.path.length)) {
        return parsed;
      }
    } catch {
    }
  }
  return payload.result || null;
}
function getSdk() {
  if (!sdkInstance) sdkInstance = new Ue();
  return sdkInstance;
}
function acceptStoredResult(expectedState, accept) {
  const stored = readStoredResult();
  if (!stored) return;
  if (expectedState && stored.state && stored.state !== expectedState) return;
  if (typeof accept === "function") accept(stored);
}
function readStoredResult() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    return JSON.parse(raw);
  } catch {
    return null;
  }
}
function loadConfig() {
  if (configPromise) return configPromise;
  configPromise = (async () => {
    try {
      const response = await fetch(pluginURL("fnos-advanced/config"), {
        credentials: "same-origin",
        cache: "no-store",
        headers: { Accept: "application/json" }
      });
      if (response.ok) {
        const data = await response.json();
        if (data && typeof data === "object") return data;
      }
    } catch {
    }
    return { appName: "Harness", authBase: "" };
  })();
  return configPromise;
}
function pluginURL(relative) {
  const base = document.baseURI.endsWith("/") ? document.baseURI : `${document.baseURI}/`;
  return new URL(relative, base).href;
}
async function resolveAuthTargets(config, settings) {
  const appName = config.appName || "Harness";
  if (await isGatewayAccess(appName)) {
    return { mode: "gateway", base: window.location.origin, via: "gateway" };
  }
  const proxy = normalizeBase(settings.proxyBase || "");
  const lan = lanBaseFromSettings(settings);
  const choices = [];
  if (proxy) {
    choices.push({ id: "proxy", label: "通过反代地址", detail: proxy, base: proxy });
  }
  if (lan && !choices.some((choice) => choice.base === lan)) {
    choices.push({ id: "lan", label: "通过内网地址", detail: lan, base: lan });
  }
  if (settings.authMode === "proxy" && proxy) {
    return { mode: "single", base: proxy, via: "proxy" };
  }
  if (settings.authMode === "lan" && lan) {
    return { mode: "single", base: lan, via: "lan" };
  }
  if (choices.length > 1) return { mode: "choose", choices };
  if (choices.length === 1) return { mode: "single", base: choices[0].base, via: choices[0].id };
  return { mode: "single", base: resolveAuthBase(), via: "auto" };
}
async function isGatewayAccess(appName) {
  if (/^\/app(?:\/|$)/.test(window.location.pathname)) return true;
  return authRouteExists(appName);
}
function lanBaseFromSettings(settings) {
  const target = String(settings.lanTarget || "").trim() || DEFAULT_SETTINGS.lanTarget;
  if (/^\d+$/.test(target)) {
    const port = Number(target);
    if (!Number.isInteger(port) || port < 1 || port > 65535) return "";
    return `http://${window.location.hostname}:${port}`;
  }
  try {
    const url = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(target) ? target : `http://${target}`);
    if (url.protocol !== "http:" && url.protocol !== "https:") return "";
    return normalizeBase(url.origin);
  } catch {
    return "";
  }
}
async function loadSettings(force = false) {
  if (!force && settingsCache && Date.now() - settingsCachedAt < 5e3) return settingsCache;
  if (!force && settingsPromise) return settingsPromise;
  const request = (async () => {
    try {
      const response = await fetch(pluginURL("fnos-advanced/settings"), {
        credentials: "same-origin",
        cache: "no-store",
        headers: { Accept: "application/json" }
      });
      if (response.ok) {
        const data = await response.json();
        if (data && data.ok && data.settings) {
          settingsCache = normalizeClientSettings(data.settings);
          settingsCachedAt = Date.now();
          settingsHostState = "ok";
          return settingsCache;
        }
        settingsHostState = "error";
      } else if (response.status === 404 || response.status === 405) {
        settingsHostState = "missing";
      } else {
        settingsHostState = "error";
      }
    } catch {
      settingsHostState = "error";
    }
    return settingsCache || { ...DEFAULT_SETTINGS };
  })();
  settingsPromise = request;
  try {
    return await request;
  } finally {
    if (settingsPromise === request) settingsPromise = null;
  }
}
function normalizeClientSettings(settings) {
  return {
    proxyBase: normalizeBase(typeof settings?.proxyBase === "string" ? settings.proxyBase : ""),
    lanTarget: typeof settings?.lanTarget === "string" && settings.lanTarget.trim() ? settings.lanTarget.trim() : DEFAULT_SETTINGS.lanTarget,
    authMode: ["ask", "proxy", "lan"].includes(settings?.authMode) ? settings.authMode : DEFAULT_SETTINGS.authMode
  };
}
async function authRouteExists(appName) {
  try {
    const response = await fetch(
      `${window.location.origin}/app-auth/pick-user-file?appName=${encodeURIComponent(appName || "Harness")}&directory=true`,
      {
        method: "GET",
        credentials: "include",
        cache: "no-store",
        redirect: "manual"
      }
    );
    if (response.type === "opaqueredirect") return true;
    return response.status !== 404 && response.status !== 502 && response.status !== 400;
  } catch {
    return false;
  }
}
function resolveAuthBase() {
  const stored = readStoredAuthBase();
  if (stored) return normalizeBase(stored);
  const { origin, pathname, protocol, hostname, port } = window.location;
  if (/^\/app(?:\/|$)/.test(pathname)) return origin;
  if (protocol === "https:" && (port === "" || port === "443")) return origin;
  if (protocol === "http:" || protocol === "https:") {
    return `${protocol}//${hostname}:5666`;
  }
  return origin;
}
function readStoredAuthBase() {
  try {
    return localStorage.getItem(AUTH_BASE_KEY) || "";
  } catch {
    return "";
  }
}
function normalizeBase(value) {
  return String(value || "").replace(/\/+$/, "");
}
function fillAndEnter(input, path) {
  input.focus();
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value")?.set;
  if (setter) setter.call(input, path);
  else input.value = path;
  input.dispatchEvent(new Event("input", { bubbles: true }));
  input.dispatchEvent(new Event("change", { bubbles: true }));
  window.setTimeout(() => {
    input.focus();
    const options = {
      key: "Enter",
      code: "Enter",
      keyCode: 13,
      which: 13,
      charCode: 13,
      bubbles: true,
      cancelable: true
    };
    input.dispatchEvent(new KeyboardEvent("keydown", options));
    input.dispatchEvent(new KeyboardEvent("keypress", options));
    input.dispatchEvent(new KeyboardEvent("keyup", options));
  }, 0);
}
function showStatus(dialog, text, isError) {
  if (getComputedStyle(dialog).position === "static") dialog.style.position = "relative";
  let status = dialog.querySelector(`.${STATUS_CLASS}`);
  if (!status) {
    status = document.createElement("div");
    status.className = STATUS_CLASS;
    status.setAttribute("role", "status");
    dialog.appendChild(status);
  }
  status.textContent = text;
  status.dataset.error = isError ? "1" : "0";
  status.style.display = "block";
  if (status.__dshFnosTimer) window.clearTimeout(status.__dshFnosTimer);
  status.__dshFnosTimer = window.setTimeout(() => {
    status.style.display = "none";
  }, isError ? 1e4 : 6e3);
}
function registerSettingsSlot(ctx) {
  if (!ctx || typeof ctx.inject !== "function") return;
  ctx.inject(["slots"], (scope) => {
    const slots = scope.slots;
    if (!slots || typeof slots.inject !== "function" || typeof slots.register !== "function") return;
    const install2 = () => slots.inject("plugins.bundle.config", () => slots.register({ name: "plugins.bundle.config", key: SETTINGS_SLOT_KEY }, SettingsSlot));
    if (typeof scope.effect === "function") {
      scope.effect(install2, "dsh-fnos-advanced: settings page");
    } else {
      install2();
    }
  });
}
function SettingsSlot(props) {
  if (props && props.view === "summary") return settingsSummary();
  return React.createElement(SettingsPage, props || {});
}
function settingsSummary() {
  const settings = settingsCache || DEFAULT_SETTINGS;
  const proxy = settings.proxyBase || "未设置";
  return `反代 ${proxy} · 内网 ${settings.lanTarget}`;
}
function SettingsPage() {
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);
  const [settings, setSettings] = React.useState({ ...DEFAULT_SETTINGS });
  const [message, setMessage] = React.useState("");
  const [error, setError] = React.useState("");
  const [embedded, setEmbedded] = React.useState(false);
  const [gatewayAccess, setGatewayAccess] = React.useState(/^\/app(?:\/|$)/.test(window.location.pathname));
  const [hostState, setHostState] = React.useState("unknown");
  const [hostVersion, setHostVersion] = React.useState("");
  React.useEffect(() => {
    let alive = true;
    loadSettings(true).then((loaded) => {
      if (!alive) return;
      setSettings(loaded);
      setHostState(settingsHostState);
      setLoading(false);
    }).catch(() => {
      if (alive) setLoading(false);
    });
    loadConfig().then((config) => {
      if (alive) setHostVersion(config.version || "");
    }).catch(() => {
    });
    const sdk = getSdk();
    sdk.ready().then(() => {
      if (alive) setEmbedded(!sdk.isStandaloneWeb);
    }).catch(() => {
    });
    if (!/^\/app(?:\/|$)/.test(window.location.pathname)) {
      loadConfig().then((config) => authRouteExists(config.appName || "Harness")).then((ok) => {
        if (alive) setGatewayAccess(ok);
      }).catch(() => {
      });
    }
    return () => {
      alive = false;
    };
  }, []);
  const update = (key) => (event) => {
    const value = event.target.value;
    setSettings((current) => ({ ...current, [key]: value }));
    setMessage("");
    setError("");
  };
  const save = async () => {
    setSaving(true);
    setMessage("");
    setError("");
    try {
      const proxyBase = validateProxyBase(settings.proxyBase);
      const response = await fetch(pluginURL("fnos-advanced/settings"), {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json", Accept: "application/json" },
        body: JSON.stringify({
          proxyBase,
          lanTarget: String(settings.lanTarget || "").trim() || DEFAULT_SETTINGS.lanTarget,
          authMode: settings.authMode
        })
      });
      if (response.status === 404 || response.status === 405) {
        throw new Error("宿主半侧未提供设置接口，通常是插件升级后没有重启 dsh。请重启 dsh 后再保存。");
      }
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.ok) throw new Error(data.error || `保存失败（HTTP ${response.status}）`);
      const saved = normalizeClientSettings(data.settings);
      settingsCache = saved;
      settingsCachedAt = Date.now();
      setSettings(saved);
      setMessage("已保存");
    } catch (err) {
      setError(err && err.message ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };
  const hostOutdated = Boolean(hostVersion && hostVersion !== VERSION);
  const hostReady = hostState === "ok" && !hostOutdated;
  const hostWarning = hostReady ? "" : hostOutdated ? `宿主半侧版本 ${hostVersion}，客户端 ${VERSION}。请重启 dsh 后再保存设置。` : hostState === "missing" ? "宿主半侧未提供设置接口，通常是插件升级后没有重启 dsh。请重启 dsh 后再保存设置。" : hostState === "error" ? "无法读取宿主设置接口，请确认 dsh 运行正常。" : "";
  const disabled = loading || saving || !hostReady;
  const inputStyle = {
    boxSizing: "border-box",
    width: "100%",
    height: 36,
    padding: "0 10px",
    borderRadius: 8,
    border: "1px solid var(--dsw-alias-border-l2, rgba(127,127,127,.32))",
    background: "transparent",
    color: "var(--dsw-alias-label-primary, inherit)",
    font: "inherit",
    fontSize: 14,
    outline: "none"
  };
  const labelStyle = {
    display: "flex",
    flexDirection: "column",
    gap: 6,
    fontSize: 13,
    color: "var(--dsw-alias-label-secondary, #666)"
  };
  const hintStyle = {
    margin: 0,
    fontSize: 12,
    lineHeight: "18px",
    color: "var(--dsw-alias-label-tertiary, #888)"
  };
  const buttonStyle = {
    boxSizing: "border-box",
    height: 36,
    padding: "0 16px",
    borderRadius: 8,
    border: "1px solid var(--dsw-alias-border-l2, rgba(127,127,127,.35))",
    background: "transparent",
    color: "var(--dsw-alias-label-primary, inherit)",
    font: "inherit",
    fontSize: 14,
    fontWeight: 500,
    cursor: disabled ? "default" : "pointer",
    opacity: disabled ? 0.6 : 1
  };
  const accessNote = embedded ? "当前运行在飞牛桌面/宿主内：会直接调用飞牛目录选择器，不打开网页授权。" : gatewayAccess ? "当前通过飞牛网关访问：直接使用当前同源地址授权，不会弹出入口选择。" : "当前不是通过飞牛网关访问：同时配置反代与内网地址后，点击「选择飞牛目录」会让你选择授权入口。";
  return React.createElement("div", {
    style: {
      display: "flex",
      flexDirection: "column",
      gap: 14,
      padding: "4px 0 10px",
      maxWidth: 640,
      color: "var(--dsw-alias-label-primary, inherit)",
      fontSize: 14
    }
  }, [
    React.createElement("div", { key: "heading", style: { display: "flex", flexDirection: "column", gap: 4 } }, [
      React.createElement("div", { key: "title", style: { fontSize: 15, fontWeight: 600 } }, "飞牛目录授权"),
      React.createElement("p", { key: "desc", style: hintStyle }, "配置非网关访问时使用的飞牛授权入口；设置保存在当前 dsh 数据目录。")
    ]),
    React.createElement("label", { key: "proxy", style: labelStyle }, [
      React.createElement("span", { key: "label" }, "反代飞牛地址"),
      React.createElement("input", {
        key: "input",
        type: "text",
        value: settings.proxyBase,
        onChange: update("proxyBase"),
        placeholder: "https://fnos.example.com",
        autoComplete: "off",
        spellCheck: false,
        disabled,
        style: inputStyle
      }),
      React.createElement("span", { key: "hint", style: hintStyle }, "填协议 + 域名[:端口]，不要带路径；留空表示未配置。")
    ]),
    React.createElement("label", { key: "lan", style: labelStyle }, [
      React.createElement("span", { key: "label" }, "飞牛内网地址 / 端口"),
      React.createElement("input", {
        key: "input",
        type: "text",
        value: settings.lanTarget,
        onChange: update("lanTarget"),
        placeholder: "5666 或 http://192.168.1.10:5666",
        autoComplete: "off",
        spellCheck: false,
        disabled,
        style: inputStyle
      }),
      React.createElement("span", { key: "hint", style: hintStyle }, "只填端口时使用当前访问主机的地址；默认 5666。")
    ]),
    React.createElement("label", { key: "mode", style: labelStyle }, [
      React.createElement("span", { key: "label" }, "非网关访问时的授权入口"),
      React.createElement("select", {
        key: "select",
        value: settings.authMode,
        onChange: update("authMode"),
        disabled,
        style: inputStyle
      }, [
        React.createElement("option", { key: "ask", value: "ask" }, "每次询问（推荐）"),
        React.createElement("option", { key: "proxy", value: "proxy" }, "优先使用反代地址"),
        React.createElement("option", { key: "lan", value: "lan" }, "优先使用内网地址")
      ])
    ]),
    React.createElement("p", { key: "access", style: hintStyle }, accessNote),
    hostWarning ? React.createElement("p", {
      key: "host-warning",
      style: { ...hintStyle, color: "var(--dsw-alias-state-error-primary, #d93025)" }
    }, hostWarning) : null,
    React.createElement("div", { key: "actions", style: { display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" } }, [
      React.createElement("button", {
        key: "save",
        type: "button",
        onClick: save,
        disabled,
        style: buttonStyle
      }, saving ? "保存中…" : "保存"),
      loading ? React.createElement("span", { key: "loading", style: hintStyle }, "读取设置…") : null,
      message ? React.createElement("span", { key: "ok", style: { fontSize: 12, color: "var(--dsw-alias-state-success-primary, #188038)" } }, message) : null,
      error ? React.createElement("span", { key: "err", style: { fontSize: 12, color: "var(--dsw-alias-state-error-primary, #d93025)" } }, error) : null
    ])
  ]);
}
function validateProxyBase(value) {
  const text = String(value || "").trim();
  if (!text) return "";
  const url = new URL(text);
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error("反代地址只支持 http/https");
  }
  return url.origin;
}
function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement("style");
  style.id = STYLE_ID;
  style.textContent = `
    .dsh-fnos-advanced-button {
      box-sizing: border-box;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      height: 36px;
      padding: 0 14px;
      border: 1px solid var(--dsw-alias-border-l2, rgba(127, 127, 127, .35));
      border-radius: var(--dsw-radius-md, 8px);
      background: transparent;
      color: var(--dsw-alias-label-primary, inherit);
      font: inherit;
      font-size: 14px;
      font-weight: 500;
      line-height: 1;
      white-space: nowrap;
      cursor: pointer;
      transition: background-color .15s ease, border-color .15s ease;
    }
    .dsh-fnos-advanced-button:hover:not(:disabled) {
      background: var(--dsw-alias-interactive-bg-hover, rgba(127, 127, 127, .1));
      border-color: var(--dsw-alias-border-l1, rgba(127, 127, 127, .5));
    }
    .dsh-fnos-advanced-button:focus-visible {
      outline: var(--dsw-focus-ring-width, 2px) solid var(--dsw-alias-state-business-primary, #4f7cff);
      outline-offset: 1px;
    }
    .dsh-fnos-advanced-button:disabled {
      opacity: .62;
      cursor: default;
    }
    .dsh-fnos-advanced-status {
      position: absolute;
      left: 24px;
      right: 24px;
      bottom: 70px;
      z-index: 10;
      display: none;
      box-sizing: border-box;
      padding: 8px 12px;
      border-radius: 8px;
      background: var(--dsw-alias-bg-layer-2, rgba(127, 127, 127, .12));
      color: var(--dsw-alias-label-secondary, inherit);
      font-size: 12px;
      line-height: 18px;
      overflow-wrap: anywhere;
      pointer-events: none;
    }
    .dsh-fnos-advanced-status[data-error="1"] {
      color: var(--dsw-alias-state-error-primary, #d93025);
    }
    .dsh-fnos-advanced-chooser {
      position: absolute;
      left: 24px;
      right: 24px;
      bottom: 70px;
      z-index: 30;
      display: flex;
      flex-direction: column;
      gap: 8px;
      box-sizing: border-box;
      padding: 12px;
      border: 1px solid var(--dsw-alias-border-l2, rgba(127, 127, 127, .32));
      border-radius: 12px;
      background: var(--dsw-alias-bg-layer-1, #fff);
      color: var(--dsw-alias-label-primary, inherit);
      box-shadow: 0 12px 40px rgba(0, 0, 0, .18);
    }
    .dsh-fnos-advanced-chooser-title {
      font-size: 13px;
      font-weight: 600;
      line-height: 18px;
    }
    .dsh-fnos-advanced-chooser-option,
    .dsh-fnos-advanced-chooser-cancel {
      box-sizing: border-box;
      display: flex;
      flex-direction: column;
      align-items: flex-start;
      gap: 2px;
      width: 100%;
      padding: 8px 10px;
      border: 1px solid var(--dsw-alias-border-l2, rgba(127, 127, 127, .32));
      border-radius: 8px;
      background: transparent;
      color: inherit;
      font: inherit;
      text-align: left;
      cursor: pointer;
    }
    .dsh-fnos-advanced-chooser-option:hover,
    .dsh-fnos-advanced-chooser-cancel:hover {
      background: var(--dsw-alias-interactive-bg-hover, rgba(127, 127, 127, .1));
    }
    .dsh-fnos-advanced-chooser-label {
      font-size: 13px;
      font-weight: 500;
      line-height: 18px;
    }
    .dsh-fnos-advanced-chooser-detail {
      max-width: 100%;
      overflow-wrap: anywhere;
      color: var(--dsw-alias-label-tertiary, #888);
      font-size: 11px;
      line-height: 16px;
    }
    .dsh-fnos-advanced-chooser-cancel {
      align-items: center;
      color: var(--dsw-alias-label-secondary, #666);
      font-size: 12px;
    }
    .dsh-fnos-advanced-toast {
      position: fixed;
      left: 50%;
      bottom: 32px;
      z-index: 10000;
      display: none;
      box-sizing: border-box;
      max-width: min(480px, calc(100vw - 32px));
      padding: 10px 14px;
      border-radius: 10px;
      background: rgba(24, 24, 28, .92);
      color: #fff;
      font-size: 13px;
      line-height: 18px;
      overflow-wrap: anywhere;
      box-shadow: 0 10px 30px rgba(0, 0, 0, .24);
      transform: translateX(-50%);
      pointer-events: none;
    }
  `;
  (document.head || document.documentElement).appendChild(style);
}
function randomId() {
  try {
    if (window.crypto && typeof window.crypto.randomUUID === "function") {
      return window.crypto.randomUUID();
    }
  } catch {
  }
  return `fnos-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}
function sleep(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}
function isVisible(element) {
  return Boolean(element && (element.offsetWidth || element.offsetHeight || element.getClientRects().length));
}
if (typeof window !== "undefined") {
  window.__dshFnosAdvanced = {
    version: VERSION,
    install,
    loadSettings,
    resolveAuthTargets,
    resolveAuthBase,
    findPathInput,
    findEditButton,
    findFooter,
    isWorkspacePicker,
    fillAndEnter
  };
}

    return module.exports;
  },
  });
})();
