package shanepark.foodbox.image.ocr.clova;

import com.google.gson.Gson;
import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Component;
import shanepark.foodbox.image.domain.DayRegion;
import shanepark.foodbox.image.domain.ParsedMenu;
import shanepark.foodbox.image.ocr.ImageMarginCalculator;
import shanepark.foodbox.image.ocr.ImageParser;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.File;
import java.io.IOException;
import java.util.*;

import static org.apache.commons.io.FileUtils.readFileToByteArray;

@RequiredArgsConstructor
@Component
public class ImageParserClova implements ImageParser {
    private final ImageMarginCalculator imageMarginCalculator;
    private final NaverClovaApi naverClovaApi;
    private final Base64.Encoder encoder = Base64.getEncoder();
    private final Gson gson = new Gson();

    @Override
    public List<ParsedMenu> parse(File file) throws IOException {
        String base64 = encoder.encodeToString(readFileToByteArray(file));
        String response = naverClovaApi.clovaRequest(base64);

        BufferedImage bufferedImage = ImageIO.read(file);
        List<DayRegion> dayRegions = imageMarginCalculator.calcParseRegions(bufferedImage);
        return parseResponse(response, dayRegions);
    }

    private List<ParsedMenu> parseResponse(String response, List<DayRegion> dayRegions) {
        JsonObject jsonObject = gson.fromJson(response, JsonObject.class);
        JsonArray images = jsonObject.getAsJsonArray("images");
        JsonObject image = images.get(0).getAsJsonObject();
        JsonArray fields = image.getAsJsonArray("fields");

        Map<DayRegion, String> dateMap = new HashMap<>();
        Map<DayRegion, List<InferTextField>> menuMap = new HashMap<>();
        for (DayRegion dayRegion : dayRegions) {
            menuMap.put(dayRegion, new ArrayList<>());
        }

        for (int i = 0; i < fields.size(); i++) {
            JsonObject field = fields.get(i).getAsJsonObject();
            JsonArray vertices = field
                    .getAsJsonObject("boundingPoly")
                    .getAsJsonArray("vertices");
            int x = vertices.get(0).getAsJsonObject().get("x").getAsInt();
            int y = vertices.get(0).getAsJsonObject().get("y").getAsInt();

            String inferText = field.get("inferText").getAsString();

            for (DayRegion dayRegion : dayRegions) {
                if (dayRegion.date().contains(x, y)) {
                    dateMap.put(dayRegion, inferText);
                    break;
                }
                if (dayRegion.menu().contains(x, y)) {
                    InferTextField inferTextField = new InferTextField(y, x, inferText);
                    menuMap.get(dayRegion).add(inferTextField);
                    break;
                }
            }
        }

        List<ParsedMenu> answer = new ArrayList<>();
        for (DayRegion day : dayRegions) {
            ParsedMenu parsedMenu = new ParsedMenu(dateMap.get(day));
            String menu = buildMenuString(menuMap.get(day));
            parsedMenu.setMenu(menu);
            answer.add(parsedMenu);
        }

        return answer;
    }

    private static String buildMenuString(List<InferTextField> inferTextFields) {
        int lastY = -1;
        StringBuilder menuBuilder = new StringBuilder();
        for (InferTextField inferTextField : inferTextFields) {
            if (Math.abs(inferTextField.y - lastY) > 10) {
                if (!menuBuilder.isEmpty()) {
                    menuBuilder.append("\n");
                }
            } else {
                menuBuilder.append(" ");
            }

            menuBuilder.append(inferTextField.inferText);
            lastY = inferTextField.y;
        }
        return menuBuilder.toString();
    }

    private record InferTextField(int y, int x, String inferText) {
    }

}
