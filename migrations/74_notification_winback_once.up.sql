-- The winback letter (a promo code for a membership that ended without a
-- single payment) goes out once per account, ever -- see
-- notification.SendWinBack. The row is the claim: of two pods handling two
-- events for one account at once, only the one whose insert succeeds mails,
-- and this index is what makes the second insert fail.
CREATE UNIQUE INDEX notification_winback_once_idx
    ON public.notification (user_id)
    WHERE key = 'winback';
