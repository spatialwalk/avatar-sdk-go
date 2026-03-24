package avatarsdkgo

import (
	"encoding/binary"
	"fmt"
	"log"
	"math/rand/v2"
	"strings"

	"github.com/hraban/opus"
)

const (
	oggOpusDefaultPreSkip   = 312
	oggOpusVendor           = "avatarsdkgo"
	oggOpusCRCPoly          = 0x04C11DB7
	opusPCMBytesPerSample   = 2
	opusEncoderChannels     = 1
	opusMaxEncodedFrameSize = 10000
)

var (
	allowedOggOpusSampleRates = map[int]struct{}{
		8000:  {},
		12000: {},
		16000: {},
		24000: {},
		48000: {},
	}
	allowedOggOpusFrameDurations = map[int]struct{}{
		10: {},
		20: {},
		40: {},
		60: {},
	}
	oggCRCTable = func() [256]uint32 {
		var table [256]uint32
		for i := range table {
			crc := uint32(i) << 24
			for bit := 0; bit < 8; bit++ {
				if crc&0x80000000 != 0 {
					crc = (crc << 1) ^ oggOpusCRCPoly
				} else {
					crc <<= 1
				}
			}
			table[i] = crc
		}

		return table
	}()
)

// EncodedAudioChunk contains a newly encoded payload and the final stream bytes, when requested.
type EncodedAudioChunk struct {
	Payload         []byte
	CompletedStream []byte
}

// OggOpusStreamEncoder incrementally encodes mono PCM audio into a continuous Ogg Opus stream.
type OggOpusStreamEncoder struct {
	sampleRate           int
	frameSize            int
	frameBytes           int
	sampleScale          int
	preSkip              int
	pcmBuffer            []byte
	pendingPacket        []byte
	pendingGranule       uint64
	headersEmitted       bool
	pageSequence         uint32
	streamSerial         uint32
	totalInputSamples    int
	collectEncodedOutput bool
	encodedOutput        []byte
	encoder              *opus.Encoder
}

// NewOggOpusStreamEncoder creates an encoder for PCM to Ogg Opus conversion.
func NewOggOpusStreamEncoder(sampleRate int, bitrate int, config *OggOpusEncoderConfig, collectEncodedOutput bool) (*OggOpusStreamEncoder, error) {
	resolved := resolveOggOpusEncoderConfig(config)
	if err := validateOggOpusEncoderConfig(sampleRate, resolved.FrameDurationMS, resolved.Application); err != nil {
		return nil, err
	}

	application, err := opusApplication(resolved.Application)
	if err != nil {
		return nil, err
	}

	encoder, err := opus.NewEncoder(sampleRate, opusEncoderChannels, application)
	if err != nil {
		return nil, fmt.Errorf("create internal Ogg Opus encoder: %w", err)
	}

	if bitrate > 0 {
		if err := encoder.SetBitrate(bitrate); err != nil {
			log.Printf("avatarsdkgo: failed to set Opus encoder bitrate %d, using encoder default: %v", bitrate, err)
		}
	}

	frameSize := sampleRate * resolved.FrameDurationMS / 1000
	return &OggOpusStreamEncoder{
		sampleRate:           sampleRate,
		frameSize:            frameSize,
		frameBytes:           frameSize * opusPCMBytesPerSample,
		sampleScale:          48000 / sampleRate,
		preSkip:              oggOpusDefaultPreSkip,
		streamSerial:         rand.Uint32(),
		collectEncodedOutput: collectEncodedOutput,
		encoder:              encoder,
	}, nil
}

// Encode consumes PCM bytes and returns the next Ogg Opus payload fragment.
func (e *OggOpusStreamEncoder) Encode(pcmData []byte, end bool) (EncodedAudioChunk, error) {
	if len(pcmData)%opusPCMBytesPerSample != 0 {
		return EncodedAudioChunk{}, fmt.Errorf("PCM input for internal Ogg Opus encoder must be 16-bit aligned")
	}

	if len(pcmData) > 0 {
		e.pcmBuffer = append(e.pcmBuffer, pcmData...)
	}

	payload := make([]byte, 0, len(pcmData))
	if err := e.encodeFullFrames(&payload); err != nil {
		return EncodedAudioChunk{}, err
	}

	if end {
		if err := e.flushFinalFrame(&payload); err != nil {
			return EncodedAudioChunk{}, err
		}
		e.finalizeStream(&payload)
	}

	chunk := EncodedAudioChunk{Payload: payload}
	if end && e.collectEncodedOutput && len(e.encodedOutput) > 0 {
		chunk.CompletedStream = append([]byte(nil), e.encodedOutput...)
	}

	return chunk, nil
}

func (e *OggOpusStreamEncoder) encodeFullFrames(payload *[]byte) error {
	for len(e.pcmBuffer) >= e.frameBytes {
		frame := append([]byte(nil), e.pcmBuffer[:e.frameBytes]...)
		e.pcmBuffer = e.pcmBuffer[e.frameBytes:]
		if err := e.queueAudioPacket(payload, frame, e.frameSize); err != nil {
			return err
		}
	}

	return nil
}

func (e *OggOpusStreamEncoder) flushFinalFrame(payload *[]byte) error {
	if len(e.pcmBuffer) == 0 {
		return nil
	}

	actualSamples := len(e.pcmBuffer) / opusPCMBytesPerSample
	frame := make([]byte, e.frameBytes)
	copy(frame, e.pcmBuffer)
	e.pcmBuffer = nil

	return e.queueAudioPacket(payload, frame, actualSamples)
}

func (e *OggOpusStreamEncoder) queueAudioPacket(payload *[]byte, pcmFrame []byte, actualSamples int) error {
	if !e.headersEmitted {
		e.emitHeaders(payload)
	}

	packet, err := e.encodePCMFrame(pcmFrame)
	if err != nil {
		return err
	}

	e.totalInputSamples += actualSamples
	granule := uint64(e.preSkip + e.totalInputSamples*e.sampleScale)

	if len(e.pendingPacket) > 0 {
		e.writePage(payload, e.pendingPacket, e.pendingGranule, false, false)
	}

	e.pendingPacket = packet
	e.pendingGranule = granule
	return nil
}

func (e *OggOpusStreamEncoder) finalizeStream(payload *[]byte) {
	if len(e.pendingPacket) > 0 {
		e.writePage(payload, e.pendingPacket, e.pendingGranule, false, true)
		e.pendingPacket = nil
		return
	}

	if e.headersEmitted {
		e.writePage(payload, nil, uint64(e.preSkip), false, true)
	}
}

func (e *OggOpusStreamEncoder) emitHeaders(payload *[]byte) {
	e.headersEmitted = true
	e.writePage(payload, e.buildOpusHead(), 0, true, false)
	e.writePage(payload, e.buildOpusTags(), 0, false, false)
}

func (e *OggOpusStreamEncoder) writePage(payload *[]byte, packet []byte, granulePosition uint64, beginOfStream bool, endOfStream bool) {
	page := e.buildOggPage(packet, granulePosition, beginOfStream, endOfStream)
	*payload = append(*payload, page...)
	if e.collectEncodedOutput {
		e.encodedOutput = append(e.encodedOutput, page...)
	}
}

func (e *OggOpusStreamEncoder) buildOggPage(packet []byte, granulePosition uint64, beginOfStream bool, endOfStream bool) []byte {
	headerType := byte(0)
	if beginOfStream {
		headerType |= 0x02
	}
	if endOfStream {
		headerType |= 0x04
	}

	lacingValues := buildOggLacingValues(packet)
	header := make([]byte, 0, 27+len(lacingValues))
	header = append(header, 'O', 'g', 'g', 'S')
	header = append(header, 0)
	header = append(header, headerType)

	buf8 := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf8, granulePosition)
	header = append(header, buf8...)

	buf4 := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf4, e.streamSerial)
	header = append(header, buf4...)
	binary.LittleEndian.PutUint32(buf4, e.pageSequence)
	header = append(header, buf4...)
	header = append(header, 0, 0, 0, 0)
	header = append(header, byte(len(lacingValues)))
	header = append(header, lacingValues...)

	page := append(header, packet...)
	binary.LittleEndian.PutUint32(page[22:26], oggCRC(page))
	e.pageSequence++

	return page
}

func (e *OggOpusStreamEncoder) buildOpusHead() []byte {
	packet := make([]byte, 0, 19)
	packet = append(packet, []byte("OpusHead")...)
	packet = append(packet, 1)
	packet = append(packet, 1)

	buf2 := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf2, uint16(e.preSkip))
	packet = append(packet, buf2...)

	buf4 := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf4, uint32(e.sampleRate))
	packet = append(packet, buf4...)

	packet = append(packet, 0, 0)
	packet = append(packet, 0)
	return packet
}

func (e *OggOpusStreamEncoder) buildOpusTags() []byte {
	packet := make([]byte, 0, 16+len(oggOpusVendor))
	packet = append(packet, []byte("OpusTags")...)

	buf4 := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf4, uint32(len(oggOpusVendor)))
	packet = append(packet, buf4...)
	packet = append(packet, []byte(oggOpusVendor)...)

	binary.LittleEndian.PutUint32(buf4, 0)
	packet = append(packet, buf4...)
	return packet
}

func (e *OggOpusStreamEncoder) encodePCMFrame(pcmFrame []byte) ([]byte, error) {
	samples := make([]int16, len(pcmFrame)/opusPCMBytesPerSample)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(pcmFrame[i*2 : i*2+2]))
	}

	packet := make([]byte, opusMaxEncodedFrameSize)
	n, err := e.encoder.Encode(samples, packet)
	if err != nil {
		return nil, fmt.Errorf("encode internal Ogg Opus frame: %w", err)
	}

	return append([]byte(nil), packet[:n]...), nil
}

func buildOggLacingValues(packet []byte) []byte {
	if len(packet) == 0 {
		return nil
	}

	size := len(packet)
	segments := make([]byte, 0, size/255+1)
	for size >= 255 {
		segments = append(segments, 255)
		size -= 255
	}

	segments = append(segments, byte(size))
	if len(packet)%255 == 0 {
		segments = append(segments, 0)
	}

	return segments
}

func validateOggOpusEncoderConfig(sampleRate int, frameDurationMS int, application OggOpusApplication) error {
	if _, ok := allowedOggOpusSampleRates[sampleRate]; !ok {
		return fmt.Errorf("internal Ogg Opus encoder supports sample rates: 8000, 12000, 16000, 24000, 48000")
	}

	if _, ok := allowedOggOpusFrameDurations[frameDurationMS]; !ok {
		return fmt.Errorf("internal Ogg Opus encoder supports frame durations: 10, 20, 40, 60 ms")
	}

	switch application {
	case OggOpusApplicationAudio, OggOpusApplicationVoIP, OggOpusApplicationRestrictedLowdelay:
		return nil
	default:
		return fmt.Errorf("internal Ogg Opus encoder application must be one of: audio, restricted_lowdelay, voip")
	}
}

func resolveOggOpusEncoderConfig(config *OggOpusEncoderConfig) OggOpusEncoderConfig {
	if config == nil {
		return OggOpusEncoderConfig{
			FrameDurationMS: 20,
			Application:     OggOpusApplicationAudio,
		}
	}

	resolved := *config
	if resolved.FrameDurationMS == 0 {
		resolved.FrameDurationMS = 20
	}
	if strings.TrimSpace(string(resolved.Application)) == "" {
		resolved.Application = OggOpusApplicationAudio
	}

	return resolved
}

func opusApplication(application OggOpusApplication) (opus.Application, error) {
	switch application {
	case OggOpusApplicationAudio:
		return opus.AppAudio, nil
	case OggOpusApplicationVoIP:
		return opus.AppVoIP, nil
	case OggOpusApplicationRestrictedLowdelay:
		return opus.AppRestrictedLowdelay, nil
	default:
		return 0, fmt.Errorf("internal Ogg Opus encoder application must be one of: audio, restricted_lowdelay, voip")
	}
}

func oggCRC(data []byte) uint32 {
	var crc uint32
	for _, b := range data {
		crc = (crc << 8) ^ oggCRCTable[byte(crc>>24)^b]
	}

	return crc
}
