package observe

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds every collector of the signal path. Labels are kept to
// source, channel, protocol and kind so cardinality stays small.
type Metrics struct {
	SourceInfo      *prometheus.GaugeVec
	SourceUp        *prometheus.GaugeVec
	SourceRestarts  *prometheus.CounterVec
	IQBlocks        *prometheus.CounterVec
	IQDropped       *prometheus.CounterVec
	IQClipping      *prometheus.GaugeVec
	PipelineLoad    *prometheus.GaugeVec
	ChannelRSSI     *prometheus.GaugeVec
	ChannelNoise    *prometheus.GaugeVec
	ChannelSNR      *prometheus.GaugeVec
	ChannelGate     *prometheus.GaugeVec
	ChannelOffset   *prometheus.GaugeVec
	DecoderMessages *prometheus.CounterVec
	DecoderEvents   *prometheus.CounterVec
	QueueDepth      *prometheus.GaugeVec
	QueueDead       *prometheus.GaugeVec
	QueueDropped    *prometheus.CounterVec
	SinkDeliveries  *prometheus.CounterVec
	SinkLatency     *prometheus.HistogramVec
	SinkHealthy     *prometheus.GaugeVec
}

// NewMetrics creates and registers the signal path collectors.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		SourceInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pagewire_source_info", Help: "Static description of a source; the value is always 1."}, []string{"source", "driver", "tuner", "serial", "usb", "center_hz", "sample_rate"}),
		SourceUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pagewire_source_up", Help: "1 while the source streams, 0 while it is being reopened."}, []string{"source"}),
		SourceRestarts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pagewire_source_restarts_total", Help: "Times a source was reopened after a failure (USB disconnect, stall)."}, []string{"source"}),
		IQBlocks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pagewire_iq_blocks_total", Help: "IQ blocks received from the source."}, []string{"source"}),
		IQDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pagewire_iq_dropped_blocks_total", Help: "IQ blocks dropped because processing fell behind."}, []string{"source"}),
		IQClipping: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pagewire_iq_clipping_ratio", Help: "Share of ADC samples at full scale in the last block (0-1)."}, []string{"source"}),
		PipelineLoad: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pagewire_pipeline_load_ratio", Help: "Processing time per block divided by the block's real-time duration."}, []string{"source"}),
		ChannelRSSI: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pagewire_channel_rssi_dbfs", Help: "Channel power in dB relative to full scale."}, []string{"channel"}),
		ChannelNoise: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pagewire_channel_noise_dbfs", Help: "Estimated channel noise floor in dBFS."}, []string{"channel"}),
		ChannelSNR: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pagewire_channel_snr_db", Help: "Channel power above the noise floor in dB."}, []string{"channel"}),
		ChannelGate: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pagewire_channel_gate_open", Help: "1 while the channel is considered active."}, []string{"channel"}),
		ChannelOffset: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pagewire_channel_freq_offset_hz", Help: "Estimated carrier offset of the last active block in Hz."}, []string{"channel"}),
		DecoderMessages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pagewire_decoder_messages_total", Help: "Decoded messages."}, []string{"channel", "protocol", "kind"}),
		DecoderEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pagewire_decoder_events_total", Help: "Decoder quality events (sync_acquired, sync_lost, bits_corrected, uncorrectable, invalid_frame)."}, []string{"channel", "protocol", "event"}),
		QueueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pagewire_queue_depth", Help: "Messages waiting for delivery."}, []string{"target"}),
		QueueDead: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pagewire_queue_dead_letters", Help: "Messages that exhausted their delivery attempts."}, []string{"target"}),
		QueueDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pagewire_queue_dropped_total", Help: "Messages discarded because the queue bound was exceeded."}, []string{"target"}),
		SinkDeliveries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pagewire_sink_deliveries_total", Help: "Delivery attempts by outcome (delivered, duplicate, filtered, skipped, rejected, error, dead)."}, []string{"target", "outcome"}),
		SinkLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "pagewire_sink_latency_seconds", Help: "Duration of delivery attempts.", Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}}, []string{"target"}),
		SinkHealthy: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pagewire_sink_healthy", Help: "1 while the last delivery or check of the target succeeded."}, []string{"target"}),
	}
	reg.MustRegister(m.SourceInfo, m.SourceUp, m.SourceRestarts, m.IQBlocks, m.IQDropped, m.IQClipping, m.PipelineLoad, m.ChannelRSSI, m.ChannelNoise, m.ChannelSNR, m.ChannelGate, m.ChannelOffset, m.DecoderMessages, m.DecoderEvents,
		m.QueueDepth, m.QueueDead, m.QueueDropped, m.SinkDeliveries, m.SinkLatency, m.SinkHealthy)
	return m
}
