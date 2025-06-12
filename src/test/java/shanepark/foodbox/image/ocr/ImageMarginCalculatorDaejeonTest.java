package shanepark.foodbox.image.ocr;

import com.google.gson.Gson;
import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import org.junit.jupiter.api.Test;
import org.springframework.core.io.ClassPathResource;
import shanepark.foodbox.image.domain.DayRegion;
import shanepark.foodbox.image.domain.ParseRegion;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.IOException;
import java.util.List;

import static org.assertj.core.api.Assertions.assertThat;

class ImageMarginCalculatorDaejeonTest {
    ImageMarginCalculator calculator = new ImageMarginCalculatorDaejeon();

    @Test
    void calcParseRegions() throws IOException {
        ClassPathResource jun9 = new ClassPathResource("menu/menu-daejeon-1000034805.jpg");
        BufferedImage image = ImageIO.read(jun9.getInputStream());

        ClassPathResource clovaResponseResource = new ClassPathResource("clova/response.json");
        String clovaResponse = new String(clovaResponseResource.getInputStream().readAllBytes());
        JsonObject jsonObject = new Gson().fromJson(clovaResponse, JsonObject.class);
        JsonArray images = jsonObject.getAsJsonArray("images");
        JsonArray fields = images.get(0).getAsJsonObject().getAsJsonArray("fields");

        // When
        List<DayRegion> dayRegions = calculator.calcParseRegions(image, fields);

        DayRegion first = dayRegions.getFirst();
        assertThat(first.date()).isEqualTo(new ParseRegion(48, 609, 155, 55));
        assertThat(first.menu()).isEqualTo(new ParseRegion(48, 664, 155, 293));

        DayRegion last = dayRegions.getLast();
        assertThat(last.date()).isEqualTo(new ParseRegion(676, 957, 155, 55));
        assertThat(last.menu()).isEqualTo(new ParseRegion(676, 1012, 155, 323));

        // Then
        assertThat(dayRegions).hasSize(10);
    }

}
