package shanepark.foodbox.image.ocr;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Component;
import shanepark.foodbox.image.domain.DayRegion;
import shanepark.foodbox.image.domain.ParseRegion;

import java.awt.image.BufferedImage;
import java.util.*;
import java.util.regex.Pattern;

@Component
@Slf4j
public class ImageMarginCalculatorEiso implements ImageMarginCalculator {

    private final Pattern DATE_PATTERN = Pattern.compile("\\d{1,2}월\\s*\\d{1,2}일");
    private final Pattern DATE_PART_PATTERN = Pattern.compile("\\d{1,2}(월|일)");
    private final Pattern DAY_NUMBER_PATTERN = Pattern.compile("\\d{1,2}");
    private final Pattern WEEKDAY_PATTERN = Pattern.compile("월요일|화요일|수요일|목요일|금요일");

    @Override
    public List<DayRegion> calcParseRegions(BufferedImage image, JsonArray fields) {
        List<WeekdayInfo> weekdayInfos = collectWeekdayInfos(fields);
        List<ColumnInfo> columns = calculateColumns(weekdayInfos, image.getWidth());

        log.info("Found {} columns", columns.size());

        List<DateInfo> dateInfos = collectDateInfos(fields, weekdayInfos);
        Map<Integer, List<DateInfo>> rowMap = groupByRow(dateInfos);
        List<Integer> sortedRows = sortedRowKeys(rowMap.keySet());

        log.info("Found {} rows", sortedRows.size());

        List<DayRegion> dayRegions = new ArrayList<>();
        List<Integer> observedMenuHeights = new ArrayList<>();

        for (int rowIdx = 0; rowIdx < sortedRows.size(); rowIdx++) {
            int rowY = sortedRows.get(rowIdx);
            List<DateInfo> rowDates = rowMap.get(rowY);

            int dateTop = rowDates.stream().mapToInt(d -> d.top).min().orElse(rowY);
            int dateBottom = rowDates.stream().mapToInt(d -> d.bottom).max().orElse(rowY + 20);
            int dateHeight = dateBottom - dateTop;

            int nextRowY = rowIdx < sortedRows.size() - 1
                    ? sortedRows.get(rowIdx + 1)
                    : image.getHeight();

            int menuStartY = dateBottom + 5;
            int menuHeightCandidate = nextRowY - menuStartY - 10;
            int menuHeight;

            if (rowIdx == sortedRows.size() - 1 && !observedMenuHeights.isEmpty()) {
                int averageHeight = (int) Math.round(observedMenuHeights.stream()
                        .mapToInt(Integer::intValue)
                        .average()
                        .orElse(menuHeightCandidate));
                menuHeight = Math.min(menuHeightCandidate, averageHeight);
            } else {
                menuHeight = menuHeightCandidate;
            }

            menuHeight = Math.max(menuHeight, 20);

            if (rowIdx < sortedRows.size() - 1) {
                observedMenuHeights.add(menuHeight);
            }

            for (ColumnInfo column : columns) {
                ParseRegion dateRegion = new ParseRegion(
                        column.startX,
                        dateTop - 5,
                        column.width,
                        dateHeight + 10
                );

                ParseRegion menuRegion = new ParseRegion(
                        column.startX,
                        menuStartY,
                        column.width,
                        menuHeight
                );

                dayRegions.add(new DayRegion(dateRegion, menuRegion));
            }
        }

        log.info("Created {} day regions ({} rows × {} columns)", dayRegions.size(), sortedRows.size(), columns.size());
        return dayRegions;
    }

    private List<WeekdayInfo> collectWeekdayInfos(JsonArray fields) {
        List<WeekdayInfo> weekdayInfos = new ArrayList<>();
        for (JsonElement element : fields) {
            JsonObject field = element.getAsJsonObject();
            String inferText = field.get("inferText").getAsString();
            if (!WEEKDAY_PATTERN.matcher(inferText).matches()) {
                continue;
            }
            JsonArray vertices = getVertices(field);
            int centerX = getMiddleX(vertices);
            int left = getLeftX(vertices);
            int right = getRightX(vertices);
            int bottom = getBottomY(vertices);
            weekdayInfos.add(new WeekdayInfo(inferText, centerX, left, right, bottom));
        }
        weekdayInfos.sort(Comparator.comparingInt(w -> w.centerX));
        if (weekdayInfos.size() != 5) {
            log.warn("Expected 5 weekdays but found {}", weekdayInfos.size());
        }
        return weekdayInfos;
    }

    private List<ColumnInfo> calculateColumns(List<WeekdayInfo> weekdayInfos, int imageWidth) {
        List<ColumnInfo> columns = new ArrayList<>();
        for (int i = 0; i < weekdayInfos.size(); i++) {
            WeekdayInfo weekday = weekdayInfos.get(i);
            int startX;
            int width;
            if (i == 0) {
                startX = 0;
                if (weekdayInfos.size() > 1) {
                    int nextCenterX = weekdayInfos.get(i + 1).centerX;
                    width = (nextCenterX + weekday.centerX) / 2 - startX;
                } else {
                    width = weekday.right - startX;
                }
            } else if (i == weekdayInfos.size() - 1) {
                int prevCenterX = weekdayInfos.get(i - 1).centerX;
                startX = (prevCenterX + weekday.centerX) / 2;
                width = imageWidth - startX;
            } else {
                int prevCenterX = weekdayInfos.get(i - 1).centerX;
                int nextCenterX = weekdayInfos.get(i + 1).centerX;
                startX = (prevCenterX + weekday.centerX) / 2;
                width = (nextCenterX + weekday.centerX) / 2 - startX;
            }
            columns.add(new ColumnInfo(i, startX, width));
        }
        return columns;
    }

    private List<DateInfo> collectDateInfos(JsonArray fields, List<WeekdayInfo> weekdayInfos) {
        List<DateInfo> dateInfos = new ArrayList<>();
        List<DateInfo> numericDateInfos = new ArrayList<>();
        int weekdayBottom = weekdayInfos.stream()
                .mapToInt(weekdayInfo -> weekdayInfo.bottom)
                .max()
                .orElse(0);
        for (JsonElement element : fields) {
            JsonObject field = element.getAsJsonObject();
            String inferText = field.get("inferText").getAsString();
            JsonArray vertices = getVertices(field);
            int y = getMiddleY(vertices);
            int top = getTopY(vertices);
            int bottom = getBottomY(vertices);

            if (DATE_PATTERN.matcher(inferText).matches()
                    || DATE_PART_PATTERN.matcher(inferText).matches()) {
                dateInfos.add(new DateInfo(inferText, y, top, bottom));
                continue;
            }

            if (isNumericDateCandidate(inferText, y, weekdayBottom)) {
                numericDateInfos.add(new DateInfo(inferText, y, top, bottom));
            }
        }
        if (dateInfos.size() < 3) {
            dateInfos.addAll(numericDateInfos);
        }
        return dateInfos;
    }

    private boolean isNumericDateCandidate(String inferText, int y, int weekdayBottom) {
        if (!DAY_NUMBER_PATTERN.matcher(inferText).matches()) {
            return false;
        }
        int day = Integer.parseInt(inferText);
        return day >= 1 && day <= 31 && y > weekdayBottom;
    }

    private List<Integer> sortedRowKeys(Set<Integer> rowKeys) {
        List<Integer> sortedRows = new ArrayList<>(rowKeys);
        sortedRows.sort(Integer::compareTo);
        return sortedRows;
    }

    private Map<Integer, List<DateInfo>> groupByRow(List<DateInfo> dateInfos) {
        Map<Integer, List<DateInfo>> rowMap = new HashMap<>();

        for (DateInfo dateInfo : dateInfos) {
            Integer rowKey = findRowKey(rowMap.keySet(), dateInfo.y);

            if (rowKey == null) {
                rowKey = dateInfo.y;
                rowMap.put(rowKey, new ArrayList<>());
            }

            rowMap.get(rowKey).add(dateInfo);
        }

        return rowMap;
    }

    private Integer findRowKey(Set<Integer> existingRows, int y) {
        for (Integer rowY : existingRows) {
            if (Math.abs(rowY - y) < 30) {
                return rowY;
            }
        }
        return null;
    }

    private JsonArray getVertices(JsonObject field) {
        return field
                .getAsJsonObject("boundingPoly")
                .getAsJsonArray("vertices");
    }

    private int getMiddleX(JsonArray vertices) {
        int sum = 0;
        for (int i = 0; i < 4; i++) {
            sum += vertices.get(i).getAsJsonObject().get("x").getAsInt();
        }
        return sum / 4;
    }

    private int getMiddleY(JsonArray vertices) {
        int sum = 0;
        for (int i = 0; i < 4; i++) {
            sum += vertices.get(i).getAsJsonObject().get("y").getAsInt();
        }
        return sum / 4;
    }

    private int getLeftX(JsonArray vertices) {
        int x0 = vertices.get(0).getAsJsonObject().get("x").getAsInt();
        int x3 = vertices.get(3).getAsJsonObject().get("x").getAsInt();
        return Math.min(x0, x3);
    }

    private int getRightX(JsonArray vertices) {
        int x1 = vertices.get(1).getAsJsonObject().get("x").getAsInt();
        int x2 = vertices.get(2).getAsJsonObject().get("x").getAsInt();
        return Math.max(x1, x2);
    }

    private int getTopY(JsonArray vertices) {
        int y0 = vertices.get(0).getAsJsonObject().get("y").getAsInt();
        int y1 = vertices.get(1).getAsJsonObject().get("y").getAsInt();
        return Math.min(y0, y1);
    }

    private int getBottomY(JsonArray vertices) {
        int y2 = vertices.get(2).getAsJsonObject().get("y").getAsInt();
        int y3 = vertices.get(3).getAsJsonObject().get("y").getAsInt();
        return Math.max(y2, y3);
    }

    private record DateInfo(String text, int y, int top, int bottom) {
    }

    private record WeekdayInfo(String text, int centerX, int left, int right, int bottom) {
    }

    private record ColumnInfo(int index, int startX, int width) {
    }
}
