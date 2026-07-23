"""
修改 2026笔记本选型比较.pptx 的主题配色为活力橙红
"""
import zipfile
import os
import shutil
import xml.etree.ElementTree as ET

src = "2026笔记本选型比较.pptx"
dst = "2026笔记本选型比较.pptx"  # 覆盖原文件
tmp = "_temp_modified.pptx"

# 读取原文件
with zipfile.ZipFile(src, 'r') as zin:
    infos = zin.infolist()
    theme_data = zin.read('ppt/theme/theme1.xml')

# 解析主题 XML
ET.register_namespace('a', 'http://schemas.openxmlformats.org/drawingml/2006/main')
root = ET.fromstring(theme_data)
ns = {'a': 'http://schemas.openxmlformats.org/drawingml/2006/main'}

# 修改颜色方案
clr_scheme = root.find('.//a:clrScheme', ns)
if clr_scheme is not None:
    clr_scheme.set('name', 'Vibrant Orange-Red')
    
    # 新颜色映射
    new_colors = {
        'dk1': ('a:sysClr', {'val': 'windowText', 'lastClr': '1A1A1A'}),
        'lt1': ('a:sysClr', {'val': 'window', 'lastClr': 'FFFFFF'}),
        'dk2': ('a:srgbClr', {'val': '8B2500'}),
        'lt2': ('a:srgbClr', {'val': 'FFF0E8'}),
        'accent1': ('a:srgbClr', {'val': 'E85D2C'}),
        'accent2': ('a:srgbClr', {'val': 'D43D1A'}),
        'accent3': ('a:srgbClr', {'val': 'FF7B33'}),
        'accent4': ('a:srgbClr', {'val': 'FFA05A'}),
        'accent5': ('a:srgbClr', {'val': 'FFD4B8'}),
        'accent6': ('a:srgbClr', {'val': 'F5F0EB'}),
        'hlink': ('a:srgbClr', {'val': 'E85D2C'}),
        'folHlink': ('a:srgbClr', {'val': 'D43D1A'}),
    }
    
    for child in list(clr_scheme):
        tag = child.tag.split('}')[-1] if '}' in child.tag else child.tag
        if tag in new_colors:
            # 清除原有子元素
            for sub in list(child):
                child.remove(sub)
            # 添加新子元素
            new_tag, attrs = new_colors[tag]
            sub = ET.SubElement(child, '{http://schemas.openxmlformats.org/drawingml/2006/main}' + new_tag.split(':')[1])
            for k, v in attrs.items():
                sub.set(k, v)

# 修改字体方案名称
font_scheme = root.find('.//a:fontScheme', ns)
if font_scheme is not None:
    font_scheme.set('name', 'Vibrant Orange-Red')

# 修改格式方案名称
fmt_scheme = root.find('.//a:fmtScheme', ns)
if fmt_scheme is not None:
    fmt_scheme.set('name', 'Vibrant Orange-Red')

# 修改 theme 名称
root.set('name', 'Vibrant Orange-Red')

# 更新 themeFamily 名称
ext = root.find('.//a:ext', ns)
if ext is not None:
    for child in ext:
        if 'themeFamily' in child.tag:
            child.set('name', 'Vibrant Orange-Red')

# 转为字节
modified_theme = ET.tostring(root, encoding='UTF-8', xml_declaration=True)

# 重新打包
with zipfile.ZipFile(src, 'r') as zin:
    with zipfile.ZipFile(tmp, 'w', zipfile.ZIP_DEFLATED) as zout:
        for info in infos:
            if info.filename == 'ppt/theme/theme1.xml':
                # 写入修改后的主题
                zout.writestr(info, modified_theme)
            else:
                zout.writestr(info, zin.read(info.filename))

# 覆盖原文件
shutil.move(tmp, src)
print("[OK] 配色已更换为 活力橙红 (Vibrant Orange-Red)")
print("颜色方案：")
print("  dk1: #1A1A1A    lt1: #FFFFFF")
print("  dk2: #8B2500    lt2: #FFF0E8")
print("   accent1: #E85D2C (主色-活力橙红)")
print("   accent2: #D43D1A (深橙红)")
print("   accent3: #FF7B33 (亮橙)")
print("   accent4: #FFA05A (浅橙)")
print("   accent5: #FFD4B8 (极浅桃)")
print("   accent6: #F5F0EB (暖灰)")