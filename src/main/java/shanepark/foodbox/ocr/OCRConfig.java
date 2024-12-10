package shanepark.foodbox.ocr;

import lombok.Getter;
import lombok.RequiredArgsConstructor;
import lombok.ToString;
import org.springframework.boot.context.properties.ConfigurationProperties;

@ConfigurationProperties(prefix = "ocr")
@RequiredArgsConstructor
@Getter
@ToString
public class OCRConfig {
    private final String secretKey;
    private final String apiUrl;
}
