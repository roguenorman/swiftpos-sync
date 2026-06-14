-- Create the products table to accept full on-prem payload states
CREATE TABLE public.products (
    swiftpos_id VARCHAR(255) PRIMARY KEY, -- Unique Identifier (ProductCode / PLU)
    name TEXT NOT NULL,                  -- Local POS receipt text title
    description TEXT NOT NULL DEFAULT '', -- Marketing copy mapped from Swiftpos WebNotes
    price NUMERIC(10,2) NOT NULL DEFAULT 0.00,
    stock_quantity INT NOT NULL DEFAULT 0,
    image_url TEXT,                      -- Left blank for cloud storage asset URLs
    is_visible BOOLEAN NOT NULL DEFAULT true,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Enable Realtime Engine on this table so Lovable updates instantly
ALTER PUBLICATION supabase_realtime ADD TABLE public.products;

-- Create high-performance indexing for the incremental high-water mark queries
CREATE INDEX idx_products_updated_at ON public.products (updated_at DESC);
