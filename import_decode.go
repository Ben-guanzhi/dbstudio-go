package main

// 导入文件的字节解码（§352）：中文 Windows 导出的 CSV 常带 UTF-8 BOM
// （Excel）或 GB18030 字节（记事本/老系统）。内核 v0.18.3 已收编
// textio.DecodingReader；此客户端在依赖同步前用同一套语义的本地实现，
// 待 tag 上 proxy 后换回内核调用即可。

import (
	"bytes"
	"io"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// decodeImportBytes 返回解码后的 reader：UTF-8 BOM 剥离、UTF-16 BOM 按其
// 字节序解码、非法 UTF-8 回退 GB18030（中文导出常见）。
func decodeImportBytes(r io.Reader) io.Reader {
	head := make([]byte, 3)
	n, _ := io.ReadFull(r, head)
	head = head[:n]
	switch {
	case len(head) == 3 && bytes.Equal(head, utf8BOM):
		return r // BOM 已消费，剩余即 UTF-8
	case len(head) >= 2 && head[0] == 0xFF && head[1] == 0xFE:
		return transform.NewReader(io.MultiReader(bytes.NewReader(head[2:]), r),
			unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder())
	case len(head) >= 2 && head[0] == 0xFE && head[1] == 0xFF:
		return transform.NewReader(io.MultiReader(bytes.NewReader(head[2:]), r),
			unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewDecoder())
	}
	sniff := make([]byte, 64*1024)
	m, _ := io.ReadFull(r, sniff)
	combined := append(head, sniff[:m]...)
	prefix := io.MultiReader(bytes.NewReader(combined), r)
	if utf8.Valid(combined) {
		return prefix
	}
	return transform.NewReader(prefix, simplifiedchinese.GB18030.NewDecoder())
}
