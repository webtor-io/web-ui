-- Populate library.name with torrent names where library.name is NULL or empty
UPDATE public.library l
SET name = tr.name 
FROM public.torrent_resource tr
WHERE l.resource_id = tr.resource_id 
  AND (l.name IS NULL OR l.name = '');