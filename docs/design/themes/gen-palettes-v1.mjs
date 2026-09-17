// 主题色板生成与对比度校验（一次性设计工具）
const h2 = n => Math.round(n).toString(16).padStart(2, '0');
function hslToHex(h, s, l) {
  s /= 100; l /= 100;
  const k = n => (n + h / 30) % 12;
  const a = s * Math.min(l, 1 - l);
  const f = n => l - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)));
  return '#' + [f(0), f(8), f(4)].map(x => h2(x * 255)).join('');
}
function hexToRgb(hex) {
  const m = hex.replace('#', '');
  return [parseInt(m.slice(0, 2), 16), parseInt(m.slice(2, 4), 16), parseInt(m.slice(4, 6), 16)];
}
function lum(hex) {
  return hexToRgb(hex).map(v => {
    v /= 255;
    return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  }).reduce((a, v, i) => a + [0.2126, 0.7152, 0.0722][i] * v, 0);
}
function contrast(a, b) {
  const [l1, l2] = [lum(a), lum(b)].sort((x, y) => y - x);
  return (l1 + 0.05) / (l2 + 0.05);
}
function mix(a, b, p) { // a 占 p
  const [r1, g1, b1] = hexToRgb(a), [r2, g2, b2] = hexToRgb(b);
  return '#' + [r1 * p + r2 * (1 - p), g1 * p + g2 * (1 - p), b1 * p + b2 * (1 - p)].map(v => h2(v)).join('');
}
const rgba = (hex, a) => { const [r, g, b] = hexToRgb(hex); return `rgba(${r}, ${g}, ${b}, ${a})`; };
const r2 = n => Math.round(n * 100) / 100;

const THEMES = {
  cobalt:   { zh: '钴蓝', hue: 226, satMul: 1.0,  lightP: '#2f5fd0', lightH: '#2750b0', darkP: '#7ba2ff', darkH: '#96b6ff', darkOn: '#0a1730' },
  graphite: { zh: '石墨', hue: 212, satMul: 0.35, lightP: '#33628c', lightH: '#2a5377', darkP: '#83aacb', darkH: '#9ebdd6', darkOn: '#0b1c28' },
  moss:     { zh: '苔原', hue: 96,  satMul: 0.8,  lightP: '#3d6d35', lightH: '#335a2c', darkP: '#9ec57c', darkH: '#b2d395', darkOn: '#12240e' },
  rust:     { zh: '赭岩', hue: 22,  satMul: 0.9,  lightP: '#a3512a', lightH: '#8a4423', darkP: '#e0946a', darkH: '#e8ab88', darkOn: '#2a1108' },
  plum:     { zh: '紫晶', hue: 258, satMul: 0.9,  lightP: '#6852c9', lightH: '#5844ad', darkP: '#ab95f5', darkH: '#bfaef8', darkOn: '#1b1040' },
  azure:    { zh: '蔚蓝·macOS', hue: 211, satMul: 0.3, lightP: '#0a63d6', lightH: '#0953ae', darkP: '#3e9dff', darkH: '#5dafff', darkOn: '#0a2a4d' },
  rose:     { zh: '玫瑰', hue: 335, satMul: 0.75, lightP: '#a93d78', lightH: '#8f3364', darkP: '#e78fc0', darkH: '#efa9d2', darkOn: '#3a0f2b' },
  ink:      { zh: '玄墨', hue: 210, satMul: 0.18, lightP: '#26292e', lightH: '#1a1d21', darkP: '#aebfd0', darkH: '#c3d0dd', darkOn: '#191c1f' },
};

function buildPalette(t) {
  const h = t.hue, sm = t.satMul;
  const L = {}, D = {};
  L.page    = hslToHex(h, 32 * sm, 96.5);
  L.panel   = '#ffffff';
  L.soft    = hslToHex(h, 28 * sm, 98);
  L.hover   = hslToHex(h, 30 * sm, 93.2);
  L.selected = mix(t.lightP, '#ffffff', 0.12);
  L.text    = hslToHex(h, 26 * sm, 12.5);
  L.muted   = hslToHex(h, 15 * sm, 43);
  L.subtle  = hslToHex(h, 13 * sm, 55.5);
  L.inverse = L.page;
  L.border  = hslToHex(h, 24 * sm, 87);
  L.strong  = hslToHex(h, 20 * sm, 77);
  L.primary = t.lightP; L.primaryH = t.lightH; L.onPrimary = '#ffffff';
  L.focus   = rgba(t.lightP, 0.26);
  L.glow    = rgba(t.lightP, 0.18);
  L.overlay = rgba(hslToHex(h, 45, 7), 0.46);
  L.shadow1 = rgba(hslToHex(h, 38, 22), 0.055);
  L.shadow2 = rgba(hslToHex(h, 38, 22), 0.085);
  D.page    = hslToHex(h, 36 * sm, 6);
  D.panel   = hslToHex(h, 34 * sm, 10.5);
  D.soft    = hslToHex(h, 32 * sm, 13.5);
  D.hover   = hslToHex(h, 34 * sm, 16.5);
  D.selected = hslToHex(h, 42 * sm, 21);
  D.text    = hslToHex(h, 30 * sm, 93);
  D.muted   = hslToHex(h, 16 * sm, 66);
  D.subtle  = hslToHex(h, 14 * sm, 52);
  D.inverse = D.page;
  D.border  = hslToHex(h, 22 * sm, 18);
  D.strong  = hslToHex(h, 20 * sm, 27);
  D.primary = t.darkP; D.primaryH = t.darkH; D.onPrimary = t.darkOn;
  D.focus   = rgba(t.darkP, 0.28);
  D.glow    = rgba(t.darkP, 0.2);
  D.overlay = rgba(hslToHex(h, 45, 3), 0.62);
  return { L, D };
}

function report(name, p) {
  const { L, D } = p;
  const rows = [
    ['L text/panel', contrast(L.text, L.panel), 4.5],
    ['L muted/panel', contrast(L.muted, L.panel), 4.5],
    ['L subtle/panel', contrast(L.subtle, L.panel), 3],
    ['L 白字/primary按钮', contrast('#ffffff', L.primary), 4],
    ['L primary作链接字/panel', contrast(L.primary, L.panel), 4.5],
    ['L text/selected行底', contrast(L.text, L.selected), 4.5],
    ['D text/panel', contrast(D.text, D.panel), 4.5],
    ['D muted/panel', contrast(D.muted, D.panel), 4.5],
    ['D subtle/panel', contrast(D.subtle, D.panel), 3],
    ['D 深字/primary按钮', contrast(D.onPrimary, D.primary), 4],
    ['D primary作链接字/panel', contrast(D.primary, D.panel), 4.5],
    ['D text/selected行底', contrast(D.text, D.selected), 4.5],
  ];
  console.log(`\n## ${name}`);
  for (const [k, v, need] of rows) console.log(`   ${k.padEnd(26)} ${r2(v)}:1  (需${need})  ${v >= need ? '✓' : '✗'}`);
}

for (const [key, t] of Object.entries(THEMES)) {
  const p = buildPalette(t);
  report(`${t.zh} (${key})`, p);
  console.log(JSON.stringify(p));
}
