# /// script
# requires-python = ">=3.10"
# dependencies = ["numpy>=1.24", "pillow>=10"]
# ///
"""Build an offline overlay report from three sets of cropped slide screenshots."""
import argparse
import json
import os
from pathlib import Path
import re
from urllib.parse import quote

import numpy as np
from PIL import Image, ImageFilter, ImageOps


def pages(folder):
    result = {}
    for path in folder.glob('p*.png'):
        match = re.fullmatch(r'p([1-9]\d*)\.png', path.name)
        if match:
            result[int(match[1])] = path
    if not result:
        raise ValueError(f'{folder} 中没有 p1.png、p2.png 等画布截图')
    return result


def edge_mask(path, size):
    with Image.open(path) as source:
        im = ImageOps.exif_transpose(source)
        # Rounding either edge by one native pixel is normal when cropping a window.
        if abs(im.width - im.height * size[0] / size[1]) > max(2, im.width * 0.002):
            raise ValueError(f'{path} 的宽高比与 PowerPoint 不一致，请检查裁切和授课比例')
        gray = np.asarray(im.convert('L').resize(size, Image.Resampling.LANCZOS), dtype='int16')
    mask = np.zeros(gray.shape, dtype=bool)
    mask[:, 1:] |= abs(np.diff(gray, axis=1)) >= 32
    mask[1:, :] |= abs(np.diff(gray, axis=0)) >= 32
    return mask


def nearby(mask):
    return np.asarray(Image.fromarray(mask.astype('uint8')).filter(ImageFilter.MaxFilter(5)), dtype=bool)


def edge_f1(reference, reference_nearby, candidate):
    if not reference.any() and not candidate.any():
        return None  # flat canvases give no evidence of alignment
    precision = (candidate & reference_nearby).sum() / max(1, candidate.sum())
    recall = (reference & nearby(candidate)).sum() / max(1, reference.sum())
    return round(100 * 2 * precision * recall / max(1e-12, precision + recall), 2)


HTML = '''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>课件画布叠图</title>
<style>body{font:16px/1.6 system-ui,sans-serif;margin:24px auto;padding:0 18px;max-width:1120px}h1{font-size:1.6rem}nav{display:flex;gap:1.3rem;align-items:center;flex-wrap:wrap;margin:1rem 0}select,input{font:inherit}figure{position:relative;margin:0;outline:1px solid #aaa}img{position:absolute;inset:0;width:100%;height:100%}#over{opacity:.5}td,th{padding:.2rem .8rem;text-align:right;font-variant-numeric:tabular-nums}summary{cursor:pointer}</style>
<h1>PowerPoint 与希沃画布叠图</h1><nav><label>页码 <select id="page"></select></label><label>希沃版本 <select id="stage"><option value="before">优化前</option><option value="after" selected>优化后</option></select></label><label>希沃透明度 <input id="alpha" type="range" min="0" max="100" value="50"> <output id="pct">50%</output></label></nav>
<figure id="canvas"><img id="ppt" alt="PowerPoint 原稿"><img id="over" alt="希沃课件"></figure><p id="score" aria-live="polite"></p><details><summary>评分方法与逐页结果</summary><p>各页按 PowerPoint 画布比例归一至 1440 像素宽。灰度相邻像素差至少 32 的位置视为边缘，横纵各容许 2 像素偏差，双向计算边缘匹配 F1。纯色页显示「无边缘」。叠图保留原始颜色。音频和动画需另行播放验证。</p><table><thead><tr><th>页</th><th>优化前</th><th>优化后</th></tr></thead><tbody id="table"></tbody></table></details>
<script>const rows=DATA;const el=id=>document.getElementById(id);const fmt=v=>v===null?'无边缘':v.toFixed(2);for(const [i,r] of rows.entries()){el('page').add(new Option(r.page,i));const tr=el('table').insertRow();for(const v of [r.page,fmt(r.before.score),fmt(r.after.score)])tr.insertCell().textContent=v;}function show(){const r=rows[Number(el('page').value)];el('canvas').style.aspectRatio=r.width+'/'+r.height;el('ppt').src=r.ref;el('over').src=r[el('stage').value].src;el('score').textContent='第 '+r.page+' 页　边缘匹配 F1　'+fmt(r.before.score)+' → '+fmt(r.after.score);}el('page').onchange=el('stage').onchange=show;el('alpha').oninput=e=>{el('over').style.opacity=e.target.value/100;el('pct').textContent=e.target.value+'%';};show();</script></html>'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('reference', 'before', 'after'):
        parser.add_argument(name, type=Path, help='包含 pN.png 截图的目录')
    parser.add_argument('-o', required=True, type=Path, help='本地 HTML 报告路径')
    args = parser.parse_args()
    reference, before, after = (pages(folder) for folder in (args.reference, args.before, args.after))
    if not reference.keys() == before.keys() == after.keys():
        parser.error('三组截图的页码集合必须一致')
    def local_url(path):
        return quote(Path(os.path.relpath(path, args.o.parent)).as_posix())
    rows = []
    for n, path in sorted(reference.items()):
        with Image.open(path) as im:
            size = (1440, max(1, round(1440 * im.height / im.width)))
        ref = edge_mask(path, size)
        ref_nearby = nearby(ref)
        row = dict(page=n, ref=local_url(path), width=size[0], height=size[1])
        for stage, candidate in [('before', before[n]), ('after', after[n])]:
            score = edge_f1(ref, ref_nearby, edge_mask(candidate, size))
            row[stage] = dict(src=local_url(candidate), score=score)
        rows.append(row)
    args.o.parent.mkdir(parents=True, exist_ok=True)
    args.o.write_text(HTML.replace('const rows=DATA;', 'const rows=' + json.dumps(rows) + ';'), encoding='utf-8')
    print(f'{len(rows)} 页 · {args.o}')


if __name__ == '__main__':
    main()
