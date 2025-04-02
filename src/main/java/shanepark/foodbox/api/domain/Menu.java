package shanepark.foodbox.api.domain;

import com.fasterxml.jackson.annotation.JsonProperty;
import lombok.Getter;
import lombok.NoArgsConstructor;

import java.time.LocalDate;
import java.util.List;

@Getter
@NoArgsConstructor
public class Menu implements Comparable<Menu> {

    private LocalDate date;
    private List<String> menus;

    @JsonProperty("valid")
    private boolean isValid;

    public Menu(LocalDate date, List<String> menus) {
        this.date = date;
        this.menus = menus;
        this.isValid = menus.size() > 2;
    }

    @Override
    public int compareTo(Menu o) {
        return this.date.compareTo(o.date);
    }
    
}
