package org.cuit.epoch.vo;

import lombok.AllArgsConstructor;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.time.LocalDateTime;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/21 18:39
 * @description: {hexo文章}
 */
@Data
@AllArgsConstructor
@NoArgsConstructor
public class HexoArticleVO extends ArticleVO {
    private LocalDateTime createTime;
}

