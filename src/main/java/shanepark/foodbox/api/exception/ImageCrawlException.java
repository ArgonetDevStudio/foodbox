package shanepark.foodbox.api.exception;

import java.io.IOException;

public class ImageCrawlException extends RuntimeException implements FoodboxException {

    public ImageCrawlException(IOException e) {
        super(e.getMessage(), e);
    }

    public ImageCrawlException(String message) {
        super(message);
    }

    @Override
    public String getErrorCode() {
        return "IMAGE_CRAWL_ERROR";
    }

    @Override
    public int getStatusCode() {
        return 500;
    }
}
