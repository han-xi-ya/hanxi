// v2 生成器：OKLCH 感知色阶 + sRGB 色域收缩映射（Radix/Fluent 标定手法）
const oklchToLin = (L, C, h) => {
  const a = C * Math.cos((h * Math.PI) / 180), b = C * Math.sin((h * Math.PI) / 180);
  const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
  const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
  const s_ = L - 0.0894841775 * a - 1.291485548 * b;
  const [l, m, s] = [l_ ** 3, m_ ** 3, s_ ** 3];
  return [
    +4.0767416621 * l - 3.3077115913 * m + 0.2309660818 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ];
};
const inGamut = rgb => rgb.every(v => v >= -0.0001 && v <= 1.0001);
const gamma = c => (c <= 0.0031308 ? 12.92 * c : 1.055 * c ** (1 / 2.4) - 0.055);
const clamp8 = v => Math.round(Math.min(1, Math.max(0, v)) * 255).toString(16).padStart(2, '0');
function oklch(L, C, h) {
  let lo = 0, hi = C, rgb = oklchToLin(L, C, h);
  if (!inGamut(rgb)) {
    for (let i = 0; i < 24; i++) {
      const mid = (lo + hi) / 2;
      if (inGamut(oklchToLin(L, mid, h))) lo = mid; else hi = mid;
    }
    rgb = oklchToLin(L, lo, h);
  }
  return '#' + rgb.map(v => clamp8(gamma(Math.max(0, v)))).join('');
}
const hexToRgb = x => { x = x.replace('#', ''); return [parseInt(x.slice(0, 2), 16), parseInt(x.slice(2, 4), 16), parseInt(x.slice(4, 6), 16)]; };
const lum = hex => hexToRgb(hex).map(v => { v /= 255; return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; })
  .reduce((a, v, i) => a + [0.2126, 0.7152, 0.0722][i] * v, 0);
const ratio = (a, b) => { const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p); return Math.round(((x + 0.05) / (y + 0.05)) * 100) / 100; };
const r2 = n => Math.round(n * 100) / 100;

// ---- v2 主题：每族定义中性面 OKLCH 台阶（L/C/H）+ 主色两点 ----
// Fluent/Radix 手法：浅色面近无色（C≤.01），深色面保留可见彩度（C .015-.02）才有"玉感"而非"死灰"；
// 深色页底 L≈.24 石墨（不是纯黑）→ 通透；强调色走 L .5-.55 高彩亮色，深字压键 → 亮眼且 AA。
const THEMES = {
  sky: {
    zh: '碧空·Fluent', hN: 255,
    L: { page: [.968, .008], panel: [1, 0], soft: [.982, .005], hover: [.944, .013], sel: [.888, .045],
         text: [.24, .02], muted: [.52, .028], subtle: [.65, .02], border: [.905, .012], strong: [.81, .02] },
    lightP: [.5, .16, 252], lightPH: [.43, .16, 252],
    D: { page: [.215, .014], panel: [.262, .016], soft: [.31, .018], hover: [.365, .022], sel: [.45, .075],
         text: [.955, .008], muted: [.77, .018], subtle: [.63, .02], border: [.37, .022], strong: [.48, .03] },
    darkP: [.76, .13, 235], darkPH: [.81, .11, 235], darkOn: [.25, .09, 245],
  },
  iris: {
    zh: '鸢尾·晨紫', hN: 300,
    L: { page: [.965, .01], panel: [1, 0], soft: [.98, .007], hover: [.94, .016], sel: [.885, .05],
         text: [.24, .025], muted: [.52, .035], subtle: [.65, .025], border: [.9, .016], strong: [.8, .028] },
    lightP: [.5, .21, 281], lightPH: [.43, .21, 281],
    D: { page: [.21, .02], panel: [.258, .024], soft: [.305, .026], hover: [.36, .03], sel: [.435, .08],
         text: [.955, .008], muted: [.77, .02], subtle: [.63, .025], border: [.365, .03], strong: [.475, .04] },
    darkP: [.76, .125, 292], darkPH: [.81, .11, 292], darkOn: [.25, .1, 295],
  },
  jade: {
    zh: '青瓷·turquoise', hN: 195,
    L: { page: [.966, .01], panel: [1, 0], soft: [.981, .007], hover: [.941, .014], sel: [.886, .042],
         text: [.24, .02], muted: [.52, .026], subtle: [.65, .02], border: [.903, .013], strong: [.805, .02] },
    lightP: [.48, .12, 188], lightPH: [.41, .12, 188],
    D: { page: [.21, .016], panel: [.256, .018], soft: [.303, .02], hover: [.358, .024], sel: [.44, .068],
         text: [.955, .008], muted: [.77, .017], subtle: [.63, .02], border: [.363, .024], strong: [.473, .032] },
    darkP: [.79, .105, 180], darkPH: [.84, .09, 180], darkOn: [.25, .07, 185],
  },
};

function build(t) {
  const Lp = {};
  for (const [k, [l, c]] of Object.entries(t.L)) Lp[k] = oklch(l, c, t.hN);
  const Dp = {};
  for (const [k, [l, c]] of Object.entries(t.D)) Dp[k] = oklch(l, c, t.hN);
  Lp.primary = oklch(...t.lightP); Lp.primaryH = oklch(...t.lightPH);
  Dp.primary = oklch(...t.darkP); Dp.primaryH = oklch(...t.darkPH); Dp.onPrimary = oklch(...t.darkOn);
  return { L: Lp, D: Dp };
}

function report(name, p) {
  const { L, D } = p;
  const rows = [
    ['L text/panel', ratio(L.text, L.panel), 4.5],
    ['L muted/panel', ratio(L.muted, L.panel), 4.5],
    ['L subtle/panel', ratio(L.subtle, L.panel), 3],
    ['L 白字/primary按钮', ratio('#ffffff', L.primary), 4.5],
    ['L primary链接/panel', ratio(L.primary, L.panel), 4.5],
    ['L text/selected', ratio(L.text, L.sel), 4.5],
    ['D text/panel', ratio(D.text, D.panel), 4.5],
    ['D muted/panel', ratio(D.muted, D.panel), 4.5],
    ['D subtle/panel', ratio(D.subtle, D.panel), 3],
    ['D 深字/primary按钮', ratio(D.onPrimary, D.primary), 4.5],
    ['D primary链接/panel', ratio(D.primary, D.panel), 4.5],
    ['D text/selected', ratio(D.text, D.sel), 4.5],
  ];
  console.log(`\n## ${name}`);
  for (const [k, v, need] of rows) console.log(`   ${k.padEnd(24)} ${r2(v)}:1 (需${need}) ${v >= need ? '✓' : '✗'}`);
  console.log(JSON.stringify(p));
}

for (const [key, t] of Object.entries(THEMES)) report(`${t.zh} (${key})`, build(t));
