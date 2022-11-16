package org.cuit.epoch.mapper;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import org.apache.ibatis.annotations.Param;
import org.cuit.epoch.entity.Tag;
import org.springframework.stereotype.Repository;

import java.util.List;

/**
 * @author: ladidol
 * @date: 2022/11/16 19:24
 * @description:
 */
@Repository
public interface TagMapper extends BaseMapper<Tag> {

//    /**
//     * 查询后台标签列表
//     *
//     * @param current   页码
//     * @param size      大小
//     * @param condition 条件
//     * @return {@link List <TagBackDTO>} 标签列表
//     */
//    List<TagBackDTO> listTagBackDTO(@Param("current") Long current, @Param("size") Long size, @Param("condition") ConditionVO condition);
//
//    /**
//     * 根据文章id查询标签名
//     *
//     * @param articleId 文章id
//     * @return {@link List<String>} 标签名列表
//     */
//    List<String> listTagNameByArticleId(Integer articleId);

}