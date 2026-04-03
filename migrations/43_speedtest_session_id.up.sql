ALTER TABLE public.speedtest_result
    ADD COLUMN session_id uuid;

CREATE INDEX speedtest_result_session_id_idx
    ON public.speedtest_result (session_id)
    WHERE session_id IS NOT NULL;
