package GoCache

import (
	"encoding/binary"
	"unsafe"
)

const (
	timestampSizeInBytes = 8
	hashSizeInBytes      = 8
	keySizeInBytes       = 2
	headersSizeInBytes   = timestampSizeInBytes + hashSizeInBytes + keySizeInBytes
)

func wrapEntry(timestamp uint64, hash uint64, key string, entry []byte, buffer *[]byte) []byte {
	keyLength := len(key)
	blobLength := len(entry) + headersSizeInBytes + keyLength

	if blobLength > len(*buffer) {
		*buffer = make([]byte, blobLength)
	}
	blob := *buffer

	binary.LittleEndian.PutUint64(blob, timestamp)
	binary.LittleEndian.PutUint64(blob[timestampSizeInBytes:], hash)
	binary.LittleEndian.PutUint16(blob[timestampSizeInBytes+hashSizeInBytes:], uint16(keyLength))
	copy(blob[headersSizeInBytes:], key)
	copy(blob[headersSizeInBytes+keyLength:], entry)

	return blob[:blobLength]
}

func appendToWrappedEntry(timestamp uint64, wrappedEntry []byte, entry []byte, buffer *[]byte) []byte {
	blobLength := len(wrappedEntry) + len(entry)
	if blobLength > len(*buffer) {
		*buffer = make([]byte, blobLength)
	}

	blob := *buffer

	binary.LittleEndian.PutUint64(blob, timestamp)
	copy(blob[timestampSizeInBytes:], wrappedEntry[timestampSizeInBytes:])
	copy(blob[len(wrappedEntry):], entry)

	return blob[:blobLength]
}

// 读出真实的value
func readEntry(data []byte) []byte {
	//key length
	length := binary.LittleEndian.Uint16(data[timestampSizeInBytes+hashSizeInBytes:])

	dst := make([]byte, len(data)-int(headersSizeInBytes+length))
	copy(dst, data[headersSizeInBytes+length:])

	return dst
}

func readTimestampFromEntry(data []byte) uint64 {
	return binary.LittleEndian.Uint64(data)
}

// 其实就是读出key
func readKeyFromEntry(data []byte) string {
	//在wrapped entry中key的长度
	//header size + key length => value
	length := binary.LittleEndian.Uint16(data[timestampSizeInBytes+hashSizeInBytes:])

	//将entry中的key读出
	dst := make([]byte, length)
	copy(dst, data[headersSizeInBytes:headersSizeInBytes+length])

	return bytesToString(dst)
}

func compareKeyFromEntry(data []byte, key string) bool {
	length := binary.LittleEndian.Uint16(data[timestampSizeInBytes+hashSizeInBytes:])

	return bytesToString(data[headersSizeInBytes:headersSizeInBytes+length]) == key
}

func readHashFromEntry(data []byte) uint64 {
	return binary.LittleEndian.Uint64(data[timestampSizeInBytes:])
}

func resetHashFromEntry(data []byte) {
	binary.LittleEndian.PutUint64(data[timestampSizeInBytes:], 0)
}

func bytesToString(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}
