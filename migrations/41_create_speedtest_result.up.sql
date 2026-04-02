CREATE TABLE public.speedtest_result (
	speedtest_result_id	uuid		DEFAULT uuid_generate_v4() NOT NULL,
	source_ip		text		NOT NULL,
	dest_ip			text		NOT NULL,
	speed_mbps		real		NOT NULL,
	request_url		text		NOT NULL,
	dest_type		text		NOT NULL,
	created_at		timestamptz	NOT NULL DEFAULT now(),
	updated_at		timestamptz	NOT NULL DEFAULT now(),
	CONSTRAINT speedtest_result_pk PRIMARY KEY (speedtest_result_id)
);

CREATE INDEX speedtest_result_created_at_idx ON public.speedtest_result (created_at DESC);
CREATE INDEX speedtest_result_source_ip_idx ON public.speedtest_result (source_ip);
CREATE INDEX speedtest_result_dest_type_idx ON public.speedtest_result (dest_type);

CREATE TRIGGER update_speedtest_result_updated_at
	BEFORE UPDATE ON public.speedtest_result
	FOR EACH ROW EXECUTE FUNCTION update_updated_at();
